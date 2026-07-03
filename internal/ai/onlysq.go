// Package ai — onlysq.go — OnlySQ API client with REAL available models.
//
// Models fetched from https://api.onlysq.ru/ai/models (tier 1-3 = user has access).
//
// ── Рекомендуемые текстовые модели ────────────────────────────────────────
//
// tier 1 (бесплатные/самые дешёвые):
//   llama-2-7b              — cost 0.0, базовая
//   llama-3.3-70b           — cost 1.0, качественная open-source
//
// tier 2 (баланс):
//   gpt-5-mini              — cost 0.5, быстрая дешёвая (рекомендуется для light)
//   gpt-5.1                 — cost 0.5, новее
//   llama3.1-8b             — cost 1.0, быстрая
//   gemini-3-flash          — cost 1.5, Google
//   claude-haiku-4-5        — cost 1.5, Anthropic
//   deepseek-r1             — cost 1.5, reasoning
//   zai-glm-4.6             — cost 1.5, Z.ai
//
// tier 3 (максимум качества):
//   gpt-5                   — cost 0.5, лучшая цена/качество
//   claude-sonnet-4-5       — cost 2.0, топ Anthropic
//   zai-glm-4.7             — cost 2.0, Z.ai последняя
//   grok-4.3                — cost 2.5, xAI
//   grok-4.20               — cost 2.5, xAI
//
// ── Image-генерация (для /imggenerate) ────────────────────────────────────
//
// tier 1:
//   flux-1-schnell              — cost 2.0, быстрая генерация
//   stable-diffusion-xl-base-1.0 — cost 2.0, SDXL
//   stable-diffusion-xl-lightning — cost 2.0, SDXL Lightning (очень быстро)
//
// tier 2:
//   flux                        — cost 2.0, Flux
//   nano-banana                 — cost 2.0, Google Nano Banana
//   recraftv3                   — cost 2.0, Recraft
//
// tier 3:
//   flux-2-dev                  — cost 2.0, Flux 2 Dev
//   flux-2-pro-preview          — cost 2.0, Flux 2 Pro
//   recraftv4                   — cost 2.0, Recraft v4
//   nano-banana-pro             — cost 2.0, Nano Banana Pro
//   grok-imagine-image          — cost 2.0, Grok Imagine
//
// ── Vision (анализ изображений) ───────────────────────────────────────────
//
// OnlySQ не помечает модели как vision явно, но gpt-5, claude-sonnet-4-5,
// gemini-3-flash поддерживают image_url в messages по OpenAI-стандарту.
// Используем gpt-5-mini для дешёвого анализа и gpt-5 для качественного.
package ai

import (
        "bytes"
        "context"
        "encoding/base64"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "time"
)

// OnlySQClient is the API client for OnlySQ.
type OnlySQClient struct {
        apiKey  string
        baseURL string
        http    *http.Client
}

// NewOnlySQClient returns a client. If apiKey is empty, all calls fail.
func NewOnlySQClient(apiKey, baseURL string) *OnlySQClient {
        if baseURL == "" {
                baseURL = "https://api.onlysq.ru/v1"
        }
        return &OnlySQClient{apiKey: apiKey, baseURL: baseURL, http: &http.Client{Timeout: 60 * time.Second}}
}

// ChatMessage is one message in the chat (text or vision).
type ChatMessage struct {
        Role    string `json:"role"`
        Content any    `json:"content"`
}

// ContentPart is one part of a multi-modal message.
type ContentPart struct {
        Type     string    `json:"type"`
        Text     string    `json:"text,omitempty"`
        ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL wraps a URL (or base64 data URI) for vision models.
type ImageURL struct {
        URL string `json:"url"`
}

// ChatRequest is the OpenAI-compatible request body.
type ChatRequest struct {
        Model       string        `json:"model"`
        Messages    []ChatMessage `json:"messages"`
        Temperature float64       `json:"temperature,omitempty"`
        MaxTokens   int           `json:"max_tokens,omitempty"`
}

// ChatResponse is the response body.
type ChatResponse struct {
        Choices []struct {
                Message struct {
                        Content string `json:"content"`
                } `json:"message"`
        } `json:"choices"`
        Error *struct {
                Message string `json:"message"`
        } `json:"error,omitempty"`
}

// Chat sends a text chat completion request.
func (c *OnlySQClient) Chat(ctx context.Context, model string, messages []ChatMessage, temp float64, maxTokens int) (string, error) {
        if c.apiKey == "" {
                return "", fmt.Errorf("ONLYSQ_API_KEY not set")
        }
        body := ChatRequest{Model: model, Messages: messages, Temperature: temp, MaxTokens: maxTokens}
        return c.doRequest(ctx, body)
}

// ChatWithImage sends a vision request with a base64-encoded image.
// model should be vision-capable: gpt-5, gpt-5-mini, claude-sonnet-4-5, gemini-3-flash.
func (c *OnlySQClient) ChatWithImage(ctx context.Context, model, prompt string, imageBytes []byte, format string, temp float64, maxTokens int) (string, error) {
        if c.apiKey == "" {
                return "", fmt.Errorf("ONLYSQ_API_KEY not set")
        }
        b64 := base64.StdEncoding.EncodeToString(imageBytes)
        dataURI := fmt.Sprintf("data:image/%s;base64,%s", format, b64)
        body := ChatRequest{
                Model: model,
                Messages: []ChatMessage{{
                        Role: "user",
                        Content: []ContentPart{
                                {Type: "text", Text: prompt},
                                {Type: "image_url", ImageURL: &ImageURL{URL: dataURI}},
                        },
                }},
                Temperature: temp,
                MaxTokens:   maxTokens,
        }
        return c.doRequest(ctx, body)
}

func (c *OnlySQClient) doRequest(ctx context.Context, body ChatRequest) (string, error) {
        bodyBytes, _ := json.Marshal(body)
        req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
        if err != nil {
                return "", err
        }
        req.Header.Set("Authorization", "Bearer "+c.apiKey)
        req.Header.Set("Content-Type", "application/json")
        resp, err := c.http.Do(req)
        if err != nil {
                return "", err
        }
        defer resp.Body.Close()
        respBytes, _ := io.ReadAll(resp.Body)
        if resp.StatusCode != 200 {
                return "", fmt.Errorf("http %d: %s", resp.StatusCode, string(respBytes))
        }
        var chat ChatResponse
        if err := json.Unmarshal(respBytes, &chat); err != nil {
                return "", err
        }
        if chat.Error != nil {
                return "", fmt.Errorf("api: %s", chat.Error.Message)
        }
        if len(chat.Choices) == 0 {
                return "", fmt.Errorf("no choices")
        }
        return chat.Choices[0].Message.Content, nil
}

// ── Default models ────────────────────────────────────────────────────────
//
// TEXT: gemini-3.5-flash (tier 2, cost 1.5) — Google, быстрая, качественная.
// IMAGE: nano-banana (tier 2, cost 2.0) — Google, отличное качество.

const DefaultTextModel = "gemini-3.5-flash"
const DefaultImageModel = "nano-banana"

// ── Готовые AI-функции ─────────────────────────────────────────────────────

// DescribeImage generates a text description of an image.
// Uses gemini-3.5-flash (tier 2, cost 1.5) — fast and good for vision.
func (c *OnlySQClient) DescribeImage(ctx context.Context, imageBytes []byte, format string) (string, error) {
        prompt := `Опиши это изображение подробно на русском языке.
Что изображено, какие цвета преобладают, какой стиль.
Ответ: 2-3 предложения.`
        return c.ChatWithImage(ctx, DefaultTextModel, prompt, imageBytes, format, 0.7, 300)
}

// ImageToPrompt analyzes an image and generates a Stable Diffusion prompt.
// Uses gemini-3.5-flash (tier 2, cost 1.5).
func (c *OnlySQClient) ImageToPrompt(ctx context.Context, imageBytes []byte, format string) (string, error) {
        prompt := `Analyze this image and generate a detailed Stable Diffusion prompt.
Include: subject, style, lighting, colors, composition, quality tags.
Format: comma-separated tags, no sentences.
Example: "a cat sitting on table, warm lighting, soft shadows, detailed fur, 4k, photorealistic"`
        return c.ChatWithImage(ctx, DefaultTextModel, prompt, imageBytes, format, 0.7, 500)
}

// GenerateColor generates a hex color from a text description.
// Uses gemini-3.5-flash (tier 2, cost 1.5).
func (c *OnlySQClient) GenerateColor(ctx context.Context, description string) (string, error) {
        resp, err := c.Chat(ctx, DefaultTextModel, []ChatMessage{
                {Role: "system", Content: "Ты генератор цветов. Верни ТОЛЬКО hex-код (#RRGGBB)."},
                {Role: "user", Content: fmt.Sprintf("Подбери цвет для: %s", description)},
        }, 0.7, 20)
        if err != nil {
                return "", err
        }
        return ExtractHex(resp)
}

// GenerateTimecycColors generates sky/sun/cloud colors from a description.
// Uses gemini-3.5-flash (tier 2, cost 1.5).
func (c *OnlySQClient) GenerateTimecycColors(ctx context.Context, description string) (map[string][3]int, error) {
        prompt := fmt.Sprintf(`Ты эксперт по настройке атмосферы в GTA SA.
На основе описания "%s" создай цветовую схему.
Верни JSON: {"SkyBottomRGB":[R,G,B],"SkyTopRGB":[R,G,B],"CloudRGB":[R,G,B],"SunCoreRGB":[R,G,B]}
RGB 0-255. ТОЛЬКО JSON без пояснений.`, description)
        resp, err := c.Chat(ctx, "gpt-5-mini", []ChatMessage{
                {Role: "system", Content: "Ты генератор JSON. Отвечай ТОЛЬКО валидным JSON."},
                {Role: "user", Content: prompt},
        }, 0.7, 200)
        if err != nil {
                return nil, err
        }
        jsonStr, err := ExtractJSON(resp)
        if err != nil {
                return nil, err
        }
        var colors map[string][3]int
        if err := json.Unmarshal([]byte(jsonStr), &colors); err != nil {
                return nil, fmt.Errorf("parse JSON: %w", err)
        }
        return colors, nil
}

// ── Image Generation ──────────────────────────────────────────────────────
//
// OnlySQ поддерживает генерацию изображений через POST /v1/images/generations
// Использует модель nano-banana (tier 2, cost 2.0) — Google, отличное качество.

// ImageGenRequest is the request body for image generation.
type ImageGenRequest struct {
        Model          string `json:"model"`
        Prompt         string `json:"prompt"`
        N              int    `json:"n,omitempty"`
        Size           string `json:"size,omitempty"` // "1024x1024", "512x512"
        ResponseFormat string `json:"response_format,omitempty"` // "url" or "b64_json"
}

// ImageGenResponse is the response from image generation.
type ImageGenResponse struct {
        Data []struct {
                URL     string `json:"url,omitempty"`
                B64JSON string `json:"b64_json,omitempty"`
        } `json:"data"`
        Error *struct {
                Message string `json:"message"`
        } `json:"error,omitempty"`
}

// GenerateImage generates an image from a text prompt.
// Uses nano-banana (tier 2, cost 2.0) by default.
// Returns the image URL (or base64 if response_format=b64_json).
func (c *OnlySQClient) GenerateImage(ctx context.Context, prompt, size string) (string, error) {
        if c.apiKey == "" {
                return "", fmt.Errorf("ONLYSQ_API_KEY not set")
        }
        if size == "" {
                size = "1024x1024"
        }
        body := ImageGenRequest{
                Model:          DefaultImageModel,
                Prompt:         prompt,
                N:              1,
                Size:           size,
                ResponseFormat: "url",
        }
        bodyBytes, _ := json.Marshal(body)
        req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/images/generations", bytes.NewReader(bodyBytes))
        if err != nil {
                return "", err
        }
        req.Header.Set("Authorization", "Bearer "+c.apiKey)
        req.Header.Set("Content-Type", "application/json")
        resp, err := c.http.Do(req)
        if err != nil {
                return "", err
        }
        defer resp.Body.Close()
        respBytes, _ := io.ReadAll(resp.Body)
        if resp.StatusCode != 200 {
                return "", fmt.Errorf("http %d: %s", resp.StatusCode, string(respBytes))
        }
        var gen ImageGenResponse
        if err := json.Unmarshal(respBytes, &gen); err != nil {
                return "", err
        }
        if gen.Error != nil {
                return "", fmt.Errorf("api: %s", gen.Error.Message)
        }
        if len(gen.Data) == 0 {
                return "", fmt.Errorf("no image in response")
        }
        if gen.Data[0].URL != "" {
                return gen.Data[0].URL, nil
        }
        return gen.Data[0].B64JSON, nil
}

// ── Доступные модели по tier'ам ────────────────────────────────────────────

// TextModels returns all available text models (tier 1-3) sorted by tier+cost.
func TextModels() []ModelInfo {
        return []ModelInfo{
                // tier 1
                {ID: "llama-2-7b", Name: "Llama 2 7B", Tier: 1, Cost: 0.0, Note: "Бесплатная, базовая"},
                {ID: "llama-3.3-70b", Name: "Llama 3.3 70B", Tier: 1, Cost: 1.0, Note: "Open-source, качественная"},
                // tier 2
                {ID: "gpt-5-mini", Name: "GPT 5 Mini", Tier: 2, Cost: 0.5, Note: "⚡ Рекомендуется (дёшево+качественно)"},
                {ID: "gpt-5.1", Name: "GPT 5.1", Tier: 2, Cost: 0.5, Note: "Новее GPT-5-mini"},
                {ID: "llama3.1-8b", Name: "Llama 3.1 8B", Tier: 2, Cost: 1.0, Note: "Быстрая"},
                {ID: "gemini-3-flash", Name: "Gemini 3 Flash", Tier: 2, Cost: 1.5, Note: "Google, быстро"},
                {ID: "claude-haiku-4-5", Name: "Claude Haiku 4.5", Tier: 2, Cost: 1.5, Note: "Anthropic, баланс"},
                {ID: "deepseek-r1", Name: "Deepseek R1", Tier: 2, Cost: 1.5, Note: "Reasoning модель"},
                {ID: "zai-glm-4.6", Name: "ZAI GLM 4.6", Tier: 2, Cost: 1.5, Note: "Z.ai, русский язык"},
                // tier 3
                {ID: "gpt-5", Name: "GPT 5", Tier: 3, Cost: 0.5, Note: "💎 Лучшее цена/качество"},
                {ID: "claude-sonnet-4-5", Name: "Claude Sonnet 4.5", Tier: 3, Cost: 2.0, Note: "Топ Anthropic"},
                {ID: "zai-glm-4.7", Name: "ZAI GLM 4.7", Tier: 3, Cost: 2.0, Note: "Z.ai последняя"},
                {ID: "grok-4.3", Name: "Grok 4.3", Tier: 3, Cost: 2.5, Note: "xAI"},
                {ID: "grok-4.20", Name: "Grok 4.20", Tier: 3, Cost: 2.5, Note: "xAI, новейшая"},
        }
}

// ImageModels returns all available image generation models (tier 1-3).
func ImageModels() []ModelInfo {
        return []ModelInfo{
                // tier 1
                {ID: "flux-1-schnell", Name: "Flux 1 Schnell", Tier: 1, Cost: 2.0, Note: "Быстрая генерация"},
                {ID: "stable-diffusion-xl-lightning", Name: "SDXL Lightning", Tier: 1, Cost: 2.0, Note: "Очень быстро"},
                // tier 2
                {ID: "flux", Name: "Flux", Tier: 2, Cost: 2.0, Note: "Flux"},
                {ID: "nano-banana", Name: "Nano Banana", Tier: 2, Cost: 2.0, Note: "Google"},
                {ID: "recraftv3", Name: "Recraft v3", Tier: 2, Cost: 2.0, Note: "Recraft"},
                // tier 3
                {ID: "flux-2-dev", Name: "Flux 2 Dev", Tier: 3, Cost: 2.0, Note: "Flux 2"},
                {ID: "flux-2-pro-preview", Name: "Flux 2 Pro", Tier: 3, Cost: 2.0, Note: "Flux 2 Pro"},
                {ID: "recraftv4", Name: "Recraft v4", Tier: 3, Cost: 2.0, Note: "Recraft v4"},
                {ID: "nano-banana-pro", Name: "Nano Banana Pro", Tier: 3, Cost: 2.0, Note: "Google Pro"},
                {ID: "grok-imagine-image", Name: "Grok Imagine", Tier: 3, Cost: 2.0, Note: "xAI"},
        }
}

// ModelInfo describes one AI model.
type ModelInfo struct {
        ID   string
        Name string
        Tier int
        Cost float64
        Note string
}
