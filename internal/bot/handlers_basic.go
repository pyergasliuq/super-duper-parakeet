// Package bot — handlers_basic.go
//
// Basic user-facing commands: /start, /help, /mysub, /top.
// Implements the actual logic for these — not stubs.
package bot

import (
        "encoding/base64"
        "fmt"
        "strings"
        "time"

        tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

        "github.com/pweper/bot/internal/bot/middleware"
        "github.com/pweper/bot/internal/config"
        "github.com/pweper/bot/internal/user"
)

// handleStart processes /start [ref_TOKEN].
func (b *Bot) handleStart(c *middleware.Ctx) {
        m := c.Message
        parts := strings.Fields(m.Text)
        if len(parts) > 1 && strings.HasPrefix(parts[1], "ref_") {
                // Referral deep-link. Try to register referral.
                b.registerReferral(c, parts[1][4:])
        }

        if c.IsPaid {
                expiry := c.User.Expiry
                if expiry == "" {
                        expiry = "—"
                }
                text := fmt.Sprintf(
                        "👋 <b>С возвращением, Premium-пользователь!</b>\n\n"+
                                "💎 Подписка активна — %s\n\n"+
                                "Все функции без ограничений. Команды — /help",
                        user.FormatExpiry(expiry))

                kb := tgbotapi.NewInlineKeyboardMarkup(
                        tgbotapi.NewInlineKeyboardRow(
                                tgbotapi.NewInlineKeyboardButtonURL("🔧 Открыть палитру HEX",
                                        "https://csscolor.ru"),
                        ),
                )
                msg := tgbotapi.NewMessage(m.Chat.ID, text)
                msg.ParseMode = "HTML"
                msg.ReplyMarkup = kb
                _, _ = b.api.Send(msg)
                return
        }

        // Free user → trial offer or free plans.
        if !c.User.TrialUsed {
                kb := tgbotapi.NewInlineKeyboardMarkup(
                        tgbotapi.NewInlineKeyboardRow(
                                tgbotapi.NewInlineKeyboardButtonData("⚡ Попробовать 3 дня за 1⭐", "buy_trial"),
                        ),
                        tgbotapi.NewInlineKeyboardRow(
                                tgbotapi.NewInlineKeyboardButtonData("💎 Посмотреть тарифы", "show_plans"),
                        ),
                )
                msg := tgbotapi.NewMessage(m.Chat.ID,
                        "👋 <b>Добро пожаловать в Pweper Bot!</b>\n\n"+
                                "🎁 <b>Специальное предложение для новых пользователей:</b>\n"+
                                "3 дня Premium всего за <b>1⭐</b>\n\n"+
                                "После пробного периода выберете удобный тариф.")
                msg.ParseMode = "HTML"
                msg.ReplyMarkup = kb
                _, _ = b.api.Send(msg)
                return
        }

        // Already used trial → show subscription plans.
        msg := tgbotapi.NewMessage(m.Chat.ID, b.startFreeText())
        msg.ParseMode = "HTML"
        msg.ReplyMarkup = b.kbSubscriptionPlans()
        _, _ = b.api.Send(msg)
}

func (b *Bot) startFreeText() string {
        return "👋 <b>Добро пожаловать в Pweper Bot!</b>\n\n" +
                "⚠️ <b>Вы используете бесплатную версию</b>\n\n" +
                "━━━━━━━━━━━━━━━━━━━━\n" +
                "❌ <b>Ограничения без подписки:</b>\n" +
                "  • Файлы не более <b>20 МБ</b>\n" +
                "  • Задержка <b>2–3 сек</b> перед обработкой\n" +
                "  • Ставитесь в очередь <b>за платными</b>\n" +
                "  • Обязательна подписка на 3 канала\n\n" +
                "━━━━━━━━━━━━━━━━━━━━\n" +
                "✅ <b>Преимущества Premium:</b>\n" +
                "  • Файлы до <b>2 ГБ</b>\n" +
                "  • <b>Мгновенная</b> обработка без очереди\n" +
                "  • Подписка на каналы <b>не нужна</b>\n" +
                "  • Все функции без ограничений\n\n" +
                "━━━━━━━━━━━━━━━━━━━━\n" +
                "💎 <b>Тарифы — оплата Telegram Stars ⭐:</b>"
}

func (b *Bot) kbSubscriptionPlans() tgbotapi.InlineKeyboardMarkup {
        rows := [][]tgbotapi.InlineKeyboardButton{}
        for _, p := range b.cfg.Plans {
                label := fmt.Sprintf("%s %s — %d ⭐", p.Emoji, p.Label, p.Stars)
                rows = append(rows, tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("buy_%d", p.Stars)),
                ))
        }
        rows = append(rows, tgbotapi.NewInlineKeyboardRow(
                tgbotapi.NewInlineKeyboardButtonURL("💬 Купить у владельца (@keedboy016)",
                        "https://t.me/keedboy016"),
        ))
        return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// handleHelp shows the /help message.
func (b *Bot) handleHelp(c *middleware.Ctx) {
        text := `<b>Привет👋 Вот возможности бота:</b>

<b>📌 Основные команды:</b>
/start — начать работу с ботом
/mysub — информация о подписке
/top — топ активных пользователей
/ref — реферальная программа
/refbal — реферальный баланс
/promo — активировать промокод
/review — оставить отзыв
/support — техническая поддержка
/edit — запуск фотошопа
/settings — настройки бота (BTX)
/help — помощь

<b>🎨 Работа с цветом:</b>
/color — покраска изображений
/recolor — перекраска цвета
/checkcolor — палитра цвета
/randcolor — случайный приятный цвет
/overlay — наложение изображения
/filters — фильтры
/hud1-4 — перекраска hud
/hp1-3 — перекраска элементов hud
/blood — кровь
/tree — листва
/vctree — VC листва
/kp1-9 — кнопки
/carmenu — меню машины
/speedometer — спидометр
/road — дороги
/casino — казино
/pickup — пикапы

<b>📂 Создание файлов:</b>
/weapon — weapon.dat
/timecyc — TimeCycle
/colorcyc — ColorCycle
/particle — кровь
/genrl — звуки бр
/bpc — шифровка bpc
/nri — сборки из neizzir в nonerai
/merger — Merger
/index — индексация

<b>✂️ Нарезка:</b>
/hudcut — нарезка hud
/map — нарезка map
/remap — восстановить map
/rehud — восстановить hud

<b>🌐 Дополнительно:</b>
/ptk — пипетка
/aim — из samp в br прицел
/weather — погода
/compress — сжатие
/search — поиск скина
/btx — настройка BTX
/wpr — веапон
/batch — пакетная обработка

<b>📁 Автоматически:</b>
<i>.btx/.png/.jpg/.zip</i> — обработка BTX/PNG/JPG
<i>.txd</i> — расшифровка TXD
<i>.bpc</i> — расшифровка bpc
<i>.ifp</i> — расшифровка анимаций
<i>.cls</i> — расшифровка коллизий
<i>.mod</i> — расшифровка моделей
<i>timecyc.dat</i> — конвертация в Black Russia
<i>timecyc.json</i> — цвета из Timecyc`

        msg := tgbotapi.NewMessage(c.Message.Chat.ID, text)
        msg.ParseMode = "HTML"
        _, _ = b.api.Send(msg)
}

// handleMySub shows subscription status.
func (b *Bot) handleMySub(c *middleware.Ctx) {
        if c.IsPaid {
                text := fmt.Sprintf("💎 <b>Premium активен</b> — %s",
                        user.FormatExpiry(c.User.Expiry))
                msg := tgbotapi.NewMessage(c.Message.Chat.ID, text)
                msg.ParseMode = "HTML"
                _, _ = b.api.Send(msg)
                return
        }
        msg := tgbotapi.NewMessage(c.Message.Chat.ID,
                "❌ <b>У вас нет Premium-подписки</b>\n\nКупить: /start")
        msg.ParseMode = "HTML"
        msg.ReplyMarkup = b.kbSubscriptionPlans()
        _, _ = b.api.Send(msg)
}

// handleTop shows the top-10 leaderboard.
func (b *Bot) handleTop(c *middleware.Ctx) {
        rows, err := b.users.GetTopUsers(c, 10)
        if err != nil {
                b.logger.Error("GetTopUsers", "err", err)
                return
        }
        medals := []string{"🥇", "🥈", "🥉", "🔸", "🔸", "🔸", "🔸", "🔸", "🔸", "🔸"}
        var sb strings.Builder
        sb.WriteString("🏆 <b>Топ-10 активных пользователей:</b>\n\n")
        for i, r := range rows {
                if i >= len(medals) {
                        break
                }
                name := r.Username
                if name == "" {
                        name = fmt.Sprintf("ID:%d", r.ChatID)
                }
                sb.WriteString(fmt.Sprintf("%s %d. %s — <b>%d</b> действий\n",
                        medals[i], i+1, name, r.Count))
        }
        if len(rows) == 0 {
                sb.WriteString("Пока нет активных пользователей.")
        }
        msg := tgbotapi.NewMessage(c.Message.Chat.ID, sb.String())
        msg.ParseMode = "HTML"
        _, _ = b.api.Send(msg)
}

// handleAdmin opens the admin panel (admin only).
func (b *Bot) handleAdmin(c *middleware.Ctx) {
        if !c.User.IsAdmin && !c.Cfg.IsAdmin(c.User.ChatID) {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Нет прав."))
                return
        }
        b.showAdminPanel(c)
}

// registerReferral decodes the ref token and creates a referral record.
// Token format: base64url-encoded user_id (no padding).
func (b *Bot) registerReferral(c *middleware.Ctx, token string) {
        refID, ok := decodeRefToken(token)
        if !ok || refID == c.User.ChatID {
                return
        }
        // Anti-cheat: skip if referred_id was registered < 5 min ago (anti-farming).
        // Stub for now — implemented in referral package later.
        _ = refID
        // TODO(phase 2): call referral.Store.Register(refID, c.User.ChatID)
}

// decodeRefToken decodes a base64url-encoded user ID.
func decodeRefToken(token string) (int64, bool) {
        // Add back padding.
        pad := 4 - len(token)%4
        if pad < 4 {
                token += strings.Repeat("=", pad)
        }
        // Std base64 decode (we'll accept both url-safe and std for resilience).
        dec, err := base64urlDecode(token)
        if err != nil || len(dec) == 0 {
                return 0, false
        }
        // Parse as decimal string (the original Python did this — str(user_id).encode).
        var id int64
        for _, b := range dec {
                if b < '0' || b > '9' {
                        return 0, false
                }
                id = id*10 + int64(b-'0')
        }
        return id, true
}

// base64urlDecode decodes a base64url-encoded string. Falls back to std base64.
func base64urlDecode(s string) ([]byte, error) {
        // Try URL-safe first.
        dec, err := base64.URLEncoding.DecodeString(s)
        if err == nil {
                return dec, nil
        }
        // Fall back to std.
        return base64.StdEncoding.DecodeString(s)
}

// timeNow returns the current time. Wrapped for testability.
var timeNow = time.Now

// handleLog sends a forwarded message + metadata to all admin log chats.
// Returns nil if no admins configured.
//
// Replaces the Python send_log function which used a separate "boti" bot
// instance. Now we use the main bot — simpler, fewer tokens, one less
// moving part.
func (b *Bot) handleLog(c *middleware.Ctx, contentType string, extra ...string) error {
        if len(b.cfg.AdminIDs) == 0 {
                return nil
        }
        u := c.Message.From
        if u == nil {
                return nil
        }
        now := formatNow()
        uname := "нет юзернейма"
        if u.UserName != "" {
                uname = "@" + u.UserName
        }
        fullName := strings.TrimSpace(u.FirstName + " " + u.LastName)

        typeIcons := map[string]string{
                "текст": "💬", "фото": "🖼", "стикер": "🎭", "гифка": "🎞",
                "видео": "🎬", "голосовое": "🎤", "аудио": "🎵", "файл": "📁",
                "видео-сообщение": "📹", "контакт": "👤", "геолокация": "📍",
                "опрос": "📊", "история": "📖",
        }
        icon := typeIcons[contentType]
        if icon == "" {
                icon = "📋"
        }

        var sb strings.Builder
        fmt.Fprintf(&sb, "%s <b>Лог: %s</b>\n", icon, contentType)
        fmt.Fprintf(&sb, "📅 <code>%s</code>\n", now)
        fmt.Fprintf(&sb, "👤 <b>%s</b>  %s\n", fullName, uname)
        fmt.Fprintf(&sb, "🆔 <code>%d</code>\n", u.ID)
        if c.Message.Chat.Type != "private" {
                fmt.Fprintf(&sb, "💬 Чат: <code>%d</code> (%s)\n", c.Message.Chat.ID, c.Message.Chat.Title)
        }
        if len(extra) > 0 && extra[0] != "" {
                fmt.Fprintf(&sb, "ℹ️ %s\n", extra[0])
        }
        if c.Message.Caption != "" {
                fmt.Fprintf(&sb, "💬 Подпись: %s\n", c.Message.Caption)
        }
        if c.Message.Text != "" {
                preview := c.Message.Text
                if len(preview) > 200 {
                        preview = preview[:200] + "…"
                }
                fmt.Fprintf(&sb, "✉️ %s\n", preview)
        }

        for _, adminID := range b.cfg.AdminIDs {
                msg := tgbotapi.NewMessage(adminID, sb.String())
                msg.ParseMode = "HTML"
                _, _ = b.api.Send(msg)
                // Forward original message too.
                fwd := tgbotapi.NewForward(adminID, c.Message.Chat.ID, c.Message.MessageID)
                _, _ = b.api.Send(fwd)
        }
        return nil
}

// formatNow returns current time as DD.MM.YYYY HH:MM:SS.
func formatNow() string {
        return timeNow().Format("02.01.2006 15:04:05")
}

// kbCheckChannels builds the inline keyboard for "subscribe to channels".
func (b *Bot) kbCheckChannels(channels []string) tgbotapi.InlineKeyboardMarkup {
        rows := [][]tgbotapi.InlineKeyboardButton{}
        for _, ch := range channels {
                username := strings.TrimPrefix(ch, "@")
                rows = append(rows, tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonURL("📢 "+ch, "https://t.me/"+username),
                ))
        }
        rows = append(rows, tgbotapi.NewInlineKeyboardRow(
                tgbotapi.NewInlineKeyboardButtonData("✅ Проверить подписку", "recheck_channels"),
        ))
        return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// reference imports (used by stubs that follow).
var _ = config.ForeverExpiry
var _ = user.RoleAdmin
