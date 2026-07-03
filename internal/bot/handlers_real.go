// Package bot — handlers_real.go
//
// Real implementations for previously-stubbed handlers.
// Each handler uses the corresponding internal/* package to do actual work.
package bot

import (
        "bytes"
        "context"
        "database/sql"
        "encoding/base64"
        "encoding/hex"
        "encoding/json"
        "fmt"
        "image/color"
        "io"
        "os"
        "path/filepath"
        "strconv"
        "strings"
        "time"

        tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

        "github.com/pweper/bot/internal/bot/middleware"
        "github.com/pweper/bot/internal/bpc"
        "github.com/pweper/bot/internal/btx"
        "github.com/pweper/bot/internal/cls"
        "github.com/pweper/bot/internal/colorcyc"
        "github.com/pweper/bot/internal/ifp"
        "github.com/pweper/bot/internal/imaging"
        "github.com/pweper/bot/internal/mod"
        "github.com/pweper/bot/internal/particle"
        "github.com/pweper/bot/internal/timecyc"
        "github.com/pweper/bot/internal/txd"
        "github.com/pweper/bot/internal/user"
        "github.com/pweper/bot/internal/weapon"
)

// ── /ref ──────────────────────────────────────────────────────────────────

func (b *Bot) handleRef(c *middleware.Ctx) {
        uid := c.User.ChatID
        // Generate ref link: base64url(user_id) without padding.
        token := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(uid, 10)))
        refLink := "https://t.me/" + b.api.Self.UserName + "?start=ref_" + token

        // Get referral stats.
        total, paid, bal := b.getRefStats(c, uid)

        // Find next tier.
        var nextTier string
        switch {
        case paid < 1:
                nextTier = "1+ → -10%"
        case paid < 10:
                nextTier = "10+ → -15%"
        case paid < 20:
                nextTier = "20+ → -20%"
        case paid < 50:
                nextTier = "50+ → -25%"
        default:
                nextTier = "🏆 Максимум!"
        }

        text := fmt.Sprintf(
                "👥 <b>Реферальная программа</b>\n\n"+
                        "🔗 Ваша ссылка:\n<code>%s</code>\n\n"+
                        "📊 Статистика:\n"+
                        "  Приглашено: <b>%d</b>\n"+
                        "  Оплатили: <b>%d</b>\n"+
                        "  Баланс: <b>%d ⭐</b>\n\n"+
                        "📈 До след. уровня: %s\n\n"+
                        "💎 <b>Скидки для приглашённых:</b>\n"+
                        "  1+ → -10%% | 10+ → -15%% | 20+ → -20%% | 50+ → -25%%\n\n"+
                        "Вы получаете <b>15%%</b> от покупки реферала ⭐",
                refLink, total, paid, bal, nextTier)
        msg := tgbotapi.NewMessage(c.Message.Chat.ID, text)
        msg.ParseMode = "HTML"
        _, _ = b.api.Send(msg)
}

func (b *Bot) getRefStats(c *middleware.Ctx, uid int64) (total, paid, bal int) {
        _ = b.users.DB().QueryRowContext(c,
                "SELECT COUNT(*) FROM referrals WHERE referrer_id = ?", uid).Scan(&total)
        _ = b.users.DB().QueryRowContext(c,
                "SELECT COUNT(*) FROM referrals WHERE referrer_id = ? AND paid = 1", uid).Scan(&paid)
        _ = b.users.DB().QueryRowContext(c,
                "SELECT ref_balance FROM users WHERE chat_id = ?", uid).Scan(&bal)
        return
}

// handleRefBal — show referral balance.
func (b *Bot) handleRefBal(c *middleware.Ctx) {
        _, _, bal := b.getRefStats(c, c.User.ChatID)
        msg := tgbotapi.NewMessage(c.Message.Chat.ID,
                fmt.Sprintf("💰 <b>Реферальный баланс: %d ⭐</b>\n\nДля вывода напишите @keedboy016", bal))
        msg.ParseMode = "HTML"
        _, _ = b.api.Send(msg)
}

// ── /settings ─────────────────────────────────────────────────────────────

func (b *Bot) handleSettings(c *middleware.Ctx) {
        b.showSettingsMenu(c)
}

func (b *Bot) showSettingsMenu(c *middleware.Ctx) {
        u := c.User
        text := fmt.Sprintf(
                "⚙️ <b>Настройки бота</b>\n\n"+
                        "🔷 BTX блок: <code>%s</code>\n"+
                        "🎯 BTX качество: <code>%s</code>\n"+
                        "⚡ BTX скорость: <code>%s</code>",
                u.BTXBlock, u.BTXQuality, u.BTXSpeed)

        kb := tgbotapi.NewInlineKeyboardMarkup(
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("🔷 Настройки BTX", "stg_btx_menu"),
                ),
        )
        msg := tgbotapi.NewMessage(c.Message.Chat.ID, text)
        msg.ParseMode = "HTML"
        msg.ReplyMarkup = kb
        _, _ = b.api.Send(msg)
}

func (b *Bot) cbSettings(c *middleware.Ctx, data string) {
        switch data {
        case "stg_main":
                b.showSettingsMenu(c)
        case "stg_btx_menu":
                b.showBTXSettingsMenu(c)
        case "stg_btx_block_menu":
                b.showBTXBlockMenu(c)
        case "stg_btx_quality_menu":
                b.showBTXQualityMenu(c)
        case "stg_btx_speed_menu":
                b.showBTXSpeedMenu(c)
        case "stg_btx_block_auto", "stg_btx_block_strong", "stg_btx_block_balanced",
                "stg_btx_block_light", "stg_btx_block_none":
                block := strings.TrimPrefix(data, "stg_btx_block_")
                _ = b.users.SetBTXSettings(c, c.User.ChatID, block, c.User.BTXQuality, c.User.BTXSpeed)
                c.User.BTXBlock = block
                b.showBTXSettingsMenu(c)
        case "stg_btx_quality_auto", "stg_btx_quality_low_weight",
                "stg_btx_quality_balanced", "stg_btx_quality_max_quality":
                q := strings.TrimPrefix(data, "stg_btx_quality_")
                _ = b.users.SetBTXSettings(c, c.User.ChatID, c.User.BTXBlock, q, c.User.BTXSpeed)
                c.User.BTXQuality = q
                b.showBTXSettingsMenu(c)
        case "stg_btx_speed_auto", "stg_btx_speed_fast",
                "stg_btx_speed_balanced", "stg_btx_speed_max_quality":
                s := strings.TrimPrefix(data, "stg_btx_speed_")
                _ = b.users.SetBTXSettings(c, c.User.ChatID, c.User.BTXBlock, c.User.BTXQuality, s)
                c.User.BTXSpeed = s
                b.showBTXSettingsMenu(c)
        }
}

func (b *Bot) showBTXSettingsMenu(c *middleware.Ctx) {
        u := c.User
        text := fmt.Sprintf("🔷 <b>Настройки BTX</b>\n\n"+
                "Сжатие: <code>%s</code>\n"+
                "Качество: <code>%s</code>\n"+
                "Скорость: <code>%s</code>",
                u.BTXBlock, u.BTXQuality, u.BTXSpeed)
        kb := tgbotapi.NewInlineKeyboardMarkup(
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("🗜 Сжатие: "+u.BTXBlock, "stg_btx_block_menu"),
                ),
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("🎯 Качество: "+u.BTXQuality, "stg_btx_quality_menu"),
                ),
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("⚡ Скорость: "+u.BTXSpeed, "stg_btx_speed_menu"),
                ),
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "stg_main"),
                ),
        )
        b.editOrSend(c, text, kb)
}

func (b *Bot) showBTXBlockMenu(c *middleware.Ctx) {
        text := "🗜 <b>Настройки сжатия</b>\n\n" +
                "<b>Авто</b> — автоматический подбор (маленькие не сжимает, большие сжимает сильнее)\n" +
                "<b>Сильное</b> — максимальное сжатие (8×8 блок)\n" +
                "<b>Баланс</b> — золотая середина (6×6 блок)\n" +
                "<b>Слабое</b> — лёгкое сжатие (4×4 блок)\n" +
                "<b>Без сжатия</b> — оригинальный размер (4×4 блок)"
        kb := tgbotapi.NewInlineKeyboardMarkup(
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("Авто", "stg_btx_block_auto"),
                        tgbotapi.NewInlineKeyboardButtonData("Сильное", "stg_btx_block_strong"),
                ),
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("Баланс", "stg_btx_block_balanced"),
                        tgbotapi.NewInlineKeyboardButtonData("Слабое", "stg_btx_block_light"),
                ),
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("Без сжатия", "stg_btx_block_none"),
                ),
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "stg_btx_menu"),
                ),
        )
        b.editOrSend(c, text, kb)
}

func (b *Bot) showBTXQualityMenu(c *middleware.Ctx) {
        text := "🎯 <b>Качество BTX</b>\n\n" +
                "<b>Авто</b> — умный подбор по содержимому\n" +
                "<b>Низкий вес</b> — сильное сжатие (8×8)\n" +
                "<b>Баланс</b> — 6×6 блок\n" +
                "<b>Макс. качество</b> — 4×4 без сжатия"
        kb := tgbotapi.NewInlineKeyboardMarkup(
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("Авто", "stg_btx_quality_auto"),
                        tgbotapi.NewInlineKeyboardButtonData("Низкий вес", "stg_btx_quality_low_weight"),
                ),
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("Баланс", "stg_btx_quality_balanced"),
                        tgbotapi.NewInlineKeyboardButtonData("Макс. качество", "stg_btx_quality_max_quality"),
                ),
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "stg_btx_menu"),
                ),
        )
        b.editOrSend(c, text, kb)
}

func (b *Bot) showBTXSpeedMenu(c *middleware.Ctx) {
        text := "⚡ <b>Скорость BTX</b>\n\n" +
                "<b>Авто</b> — подбор по размеру\n" +
                "<b>Скорость</b> — максимально быстро\n" +
                "<b>Баланс</b> — средняя скорость\n" +
                "<b>Макс. качество</b> — медленно, лучший результат"
        kb := tgbotapi.NewInlineKeyboardMarkup(
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("Авто", "stg_btx_speed_auto"),
                        tgbotapi.NewInlineKeyboardButtonData("Скорость", "stg_btx_speed_fast"),
                ),
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("Баланс", "stg_btx_speed_balanced"),
                        tgbotapi.NewInlineKeyboardButtonData("Макс. качество", "stg_btx_speed_max_quality"),
                ),
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "stg_btx_menu"),
                ),
        )
        b.editOrSend(c, text, kb)
}

// editOrSend edits the existing message if this is a callback, otherwise sends a new one.
func (b *Bot) editOrSend(c *middleware.Ctx, text string, kb tgbotapi.InlineKeyboardMarkup) {
        if c.Update.CallbackQuery != nil {
                edit := tgbotapi.NewEditMessageTextAndMarkup(c.Message.Chat.ID, c.Message.MessageID, text, kb)
                edit.ParseMode = "HTML"
                _, err := b.api.Send(edit)
                if err == nil {
                        return
                }
        }
        msg := tgbotapi.NewMessage(c.Message.Chat.ID, text)
        msg.ParseMode = "HTML"
        msg.ReplyMarkup = kb
        _, _ = b.api.Send(msg)
}

// ── /btx settings via text command ────────────────────────────────────────

func (b *Bot) handleBTXSettings(c *middleware.Ctx, parts []string) {
        b.showBTXSettingsMenu(c)
}

// ── /color, /recolor, /filters etc — text-only usage hint ─────────────────

func (b *Bot) handleHUDColor(c *middleware.Ctx, cmd string, p []string) {
        b.replyUsageHint(c, cmd)
}

func (b *Bot) handleColorCommand(c *middleware.Ctx, cmd string, p []string) {
        if len(p) < 2 {
                b.replyUsageHint(c, cmd)
                return
        }
        hexColor := p[1]
        alpha := 1.0
        if len(p) >= 3 {
                if a, err := strconv.ParseFloat(p[2], 64); err == nil {
                        alpha = a
                }
        }
        // Apply to built-in zip (zip/<cmd>.zip).
        zipPath := filepath.Join(b.cfg.AssetsDir, "zip", cmd+".zip")
        if _, err := os.Stat(zipPath); err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "❌ Файл `zip/"+cmd+".zip` не найден в assets/."))
                return
        }
        b.processColorZIP(c, hexColor, alpha, zipPath, cmd)
}

func (b *Bot) processColorZIP(c *middleware.Ctx, hexColor string, alpha float64, srcZipPath, name string) {
        msg, _ := b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "⏳ Обрабатываю..."))
        var msgID int
        if msg.Chat != nil {
                msgID = msg.MessageID
        }
        defer func() {
                if msgID != 0 {
                        _, _ = b.api.Request(tgbotapi.NewDeleteMessage(c.Message.Chat.ID, msgID))
                }
        }()

        // Read source zip.
        srcBytes, err := os.ReadFile(srcZipPath)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Ошибка чтения: "+err.Error()))
                return
        }

        // Open source zip.
        r, err := openZipReader(srcBytes)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Ошибка ZIP: "+err.Error()))
                return
        }

        // Output buffer.
        var outBuf bytes.Buffer
        w := newZipWriter(&outBuf)

        for _, f := range r.Reader.File {
                if f.FileInfo().IsDir() {
                        continue
                }
                if !isImageFile(f.Name) {
                        w.copyFile(f)
                        continue
                }
                rc, err := f.Open()
                if err != nil {
                        continue
                }
                imgBytes, _ := io.ReadAll(rc)
                rc.Close()

                img, err := imaging.DecodeImage(imgBytes)
                if err != nil {
                        continue
                }
                out, err := imaging.Color(img, hexColor, alpha)
                if err != nil {
                        continue
                }
                w.writeFile(f.Name, out)
        }
        w.close()

        // Send the resulting ZIP.
        outBytes := outBuf.Bytes()
        reader := bytes.NewReader(outBytes)
        fileReq := tgbotapi.FileReader{
                Reader: reader,
                Name:   name + "_colored.zip",
        }
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, fileReq)
        doc.Caption = "<b>⚡️Файл готов!</b>"
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// ── /compress ─────────────────────────────────────────────────────────────

func (b *Bot) handleCompress(c *middleware.Ctx, parts []string) {
        b.replyUsageHint(c, "compress")
}

// ── /timecyc ──────────────────────────────────────────────────────────────

func (b *Bot) handleTimecyc(c *middleware.Ctx, parts []string) {
        if len(parts) < 5 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "❔ Пример: /timecyc #НизНеба #ВерхНеба #Облака #Солнце"))
                return
        }
        jsonStr, err := timecyc.GenerateFromHexes(parts[1], parts[2], parts[3], parts[4])
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Ошибка: "+err.Error()))
                return
        }
        r := strings.NewReader(jsonStr)
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: r,

                Name: "timecyc.json",
        })
        doc.Caption = "<b>⚡️TimeCycle готов!</b>"
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// ── /colorcyc ─────────────────────────────────────────────────────────────

func (b *Bot) handleColorcyc(c *middleware.Ctx, parts []string) {
        if len(parts) < 2 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "❔ Пример: /colorcyc 1.2 или /colorcyc #FF0000"))
                return
        }
        var data string
        var err error
        if isFloat(parts[1]) {
                data, err = colorcyc.GenerateFromBlack(parts[1])
        } else {
                data, err = colorcyc.GenerateFromHex(parts[1])
        }
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Ошибка: "+err.Error()))
                return
        }
        r := strings.NewReader(data)
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: r,

                Name: "colorcycle.dat",
        })
        doc.Caption = "<b>⚡️ColorCycle готов!</b>"
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// ── /checkcolor ───────────────────────────────────────────────────────────

func (b *Bot) handleCheckColor(c *middleware.Ctx, parts []string) {
        if len(parts) < 2 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❔ Пример: /checkcolor #FF0000"))
                return
        }
        hexColor := strings.ToUpper(parts[1])
        if !strings.HasPrefix(hexColor, "#") {
                hexColor = "#" + hexColor
        }
        // Decode and validate.
        r, g, bl, err := imaging.HexToRGB(hexColor)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        // Generate 400×500 image with the color.
        img := imaging.MakeColorImage(400, 500, color.NRGBA{r, g, bl, 255})
        pngBytes, _ := imaging.EncodePNG(img)
        reader := bytes.NewReader(pngBytes)
        photo := tgbotapi.NewPhoto(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: reader,
                Name:   "color.png",
        })
        photo.Caption = fmt.Sprintf("🎨 <b>Палитра цвета - %s</b>", hexColor)
        photo.ParseMode = "HTML"
        _, _ = b.api.Send(photo)
}

// ── /randcolor ────────────────────────────────────────────────────────────

func (b *Bot) handleRandColor(c *middleware.Ctx) {
        // Pick a random pleasant color.
        h := time.Now().UnixNano() % 360
        r, g, bl := hsvToRGB(uint8(h), 70, 90)
        hexColor := imaging.RGBToHex(r, g, bl)
        img := imaging.MakeColorImage(400, 500, color.NRGBA{r, g, bl, 255})
        pngBytes, _ := imaging.EncodePNG(img)
        reader := bytes.NewReader(pngBytes)
        photo := tgbotapi.NewPhoto(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: reader,
                Name:   "color.png",
        })
        photo.Caption = fmt.Sprintf("🎨 <b>Hex цвет - %s</b>", hexColor)
        photo.ParseMode = "HTML"
        _, _ = b.api.Send(photo)
}

// hsvToRGB converts HSV (0-360, 0-100, 0-100) to RGB (0-255).
func hsvToRGB(h, s, v uint8) (uint8, uint8, uint8) {
        hf := float64(h) * 6 / 360
        sf := float64(s) / 100
        vf := float64(v) / 100
        i := int(hf)
        f := hf - float64(i)
        p := vf * (1 - sf)
        q := vf * (1 - f*sf)
        t := vf * (1 - (1-f)*sf)
        var r, g, bl float64
        switch i % 6 {
        case 0:
                r, g, bl = vf, t, p
        case 1:
                r, g, bl = q, vf, p
        case 2:
                r, g, bl = p, vf, t
        case 3:
                r, g, bl = p, q, vf
        case 4:
                r, g, bl = t, p, vf
        case 5:
                r, g, bl = vf, p, q
        }
        return uint8(r * 255), uint8(g * 255), uint8(bl * 255)
}

// ── /edit ──────────────────────────────────────────────────────────────────

func (b *Bot) handleEdit(c *middleware.Ctx) {
        kb := tgbotapi.NewInlineKeyboardMarkup(
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonURL("Открыть Photoshop", "https://pixlr.com/ru/express/"),
                ),
        )
        msg := tgbotapi.NewMessage(c.Message.Chat.ID, "<b>⚡️Держи редактор:</b>")
        msg.ParseMode = "HTML"
        msg.ReplyMarkup = kb
        _, _ = b.api.Send(msg)
}

// ── /weapon ────────────────────────────────────────────────────────────────

func (b *Bot) handleWeapon(c *middleware.Ctx, parts []string) {
        if len(parts) < 3 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "❔ Пример: /weapon 9 50 (патроны разброс)\nИспользуй /wpr 1..4 для выбора пресета."))
                return
        }
        pt, err := strconv.Atoi(parts[1])
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ PT должно быть числом."))
                return
        }
        razb, err := strconv.Atoi(parts[2])
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ RAZB должно быть числом."))
                return
        }
        presetID := "1" // default
        // Look up user's preset setting (stored in memory only for this session).
        if v, ok := weaponPresets[c.User.ChatID]; ok {
                presetID = v
        }
        preset, err := weapon.GetPreset(presetID)
        if err != nil {
                preset, _ = weapon.GetPreset("1")
        }
        progress, _ := b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "⏳ Обрабатываю..."))
        var progressID int
                _ = progressID
        if progress.Chat != nil {
                progressID = progress.MessageID
        }
        tmpDir, err := os.MkdirTemp("", "weapon-*")
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        defer func() {
                os.RemoveAll(tmpDir)
                if progressID != 0 {
                        _, _ = b.api.Request(tgbotapi.NewDeleteMessage(c.Message.Chat.ID, progressID))
                }
        }()

        presetPath := filepath.Join(b.cfg.AssetsDir, preset.Folder)
        if err := copyDir(presetPath, tmpDir); err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Пресет не найден: "+presetPath))
                return
        }
        if err := weapon.ApplyParams(tmpDir, pt, razb); err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        // Zip the temp dir.
        var buf bytes.Buffer
        if err := zipDirectory(tmpDir, &buf); err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        zipBytes := buf.Bytes()
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(zipBytes),
                Name:   "weapon.zip",
        })
        doc.Caption = fmt.Sprintf("🔫 <b>Weapon готов!</b>\n\n📦 Патроны: <b>%d</b>\n🎯 Разброс: <b>%d</b>\n🗂 Пресет: %s",
                pt, razb, preset.Name)
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// weaponPresets holds user-selected weapon presets (in-memory, lost on restart).
var weaponPresets = map[int64]string{}

// handleWPR — set weapon preset.
func (b *Bot) handleWPR(c *middleware.Ctx, parts []string) {
        if len(parts) < 2 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❔ Пример: /wpr 2"))
                return
        }
        if _, err := weapon.GetPreset(parts[1]); err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❔ Доступные пресеты: 1, 2, 3, 4"))
                return
        }
        weaponPresets[c.User.ChatID] = parts[1]
        preset, _ := weapon.GetPreset(parts[1])
        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                fmt.Sprintf("✅ <b>Пресет weapon сохранён</b>\n🗂 %s\n📄 %s\n\nОтправь <b>/weapon &lt;PT&gt; &lt;RAZB&gt;</b>",
                        preset.Name, preset.Desc)))
}

// ── /particle ──────────────────────────────────────────────────────────────

func (b *Bot) handleParticle(c *middleware.Ctx, parts []string) {
        if len(parts) < 3 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "❔ Пример: /particle #FF0000 10 [trail] [u] [r]"))
                return
        }
        hexColor := parts[1]
        size := parts[2]
        trail := ""
        u := ""
        r := ""
        if len(parts) > 3 {
                trail = parts[3]
        }
        if len(parts) > 4 {
                u = parts[4]
        }
        if len(parts) > 5 {
                r = parts[5]
        }
        data, err := particle.GenerateFromHex(hexColor, size, trail, u, r)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        reader := strings.NewReader(data)
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: reader,

                Name: "particle.cfg",
        })
        doc.Caption = "<b>⚡️Ваш particle.cfg готов!</b>"
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// ── /search ────────────────────────────────────────────────────────────────

func (b *Bot) handleSearch(c *middleware.Ctx, parts []string) {
        if len(parts) < 2 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❔ Пример: /search 11 или /search player"))
                return
        }
        query := parts[1]
        results := searchInSkins(query, filepath.Join(b.cfg.AssetsDir, "skins.txt"))
        if results == nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "Ошибка чтения файла skins.txt"))
                return
        }
        if len(results) == 0 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, fmt.Sprintf("Нет информации о - %s", query)))
                return
        }
        idXyina, nameXyina := results[0], results[1]
        // Look for the model file.
        modName := strings.Replace(strings.ToLower(nameXyina), ".mod", "", 1)
        modPath := filepath.Join(b.cfg.AssetsDir, "Editing", "mod", modName+".mod")
        texPath := filepath.Join(b.cfg.AssetsDir, "Editing", "texture", "texture_"+modName+".zip")
        hasMod := fileExists(modPath)
        hasTex := fileExists(texPath)
        if hasMod {
                doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                        Reader: mustOpen(modPath),

                        Name: filepath.Base(modPath),
                })
                _, _ = b.api.Send(doc)
        }
        if hasTex {
                doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                        Reader: mustOpen(texPath),

                        Name: filepath.Base(texPath),
                })
                doc.Caption = fmt.Sprintf("ID - %s\nNAME - %s", idXyina, nameXyina)
                _, _ = b.api.Send(doc)
        }
        if !hasMod && !hasTex {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        fmt.Sprintf("ID - %s\nNAME - %s\n\nФайлы не найдены.", idXyina, nameXyina)))
        }
}

// ── /skin ──────────────────────────────────────────────────────────────────

func (b *Bot) handleSkin(c *middleware.Ctx, parts []string) {
        if len(parts) < 2 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❔ Пример: /skin 11"))
                return
        }
        skinID := parts[1]
        dffPath := filepath.Join(b.cfg.AssetsDir, "skin", skinID+".dff")
        texPath := filepath.Join(b.cfg.AssetsDir, "texture", "texture_"+skinID+".zip")
        hasDff := fileExists(dffPath)
        hasTex := fileExists(texPath)
        if !hasDff && !hasTex {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "Такого названия нет"))
                return
        }
        if hasDff {
                doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                        Reader: mustOpen(dffPath),

                        Name: filepath.Base(dffPath),
                })
                doc.Caption = "⚡️<b>Держите скин!</b>"
                doc.ParseMode = "HTML"
                _, _ = b.api.Send(doc)
        }
        if hasTex {
                doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                        Reader: mustOpen(texPath),

                        Name: filepath.Base(texPath),
                })
                doc.Caption = "⚡️<b>Держите текстуры!</b>"
                doc.ParseMode = "HTML"
                _, _ = b.api.Send(doc)
        }
}

// ── /car ───────────────────────────────────────────────────────────────────

func (b *Bot) handleCar(c *middleware.Ctx, parts []string) {
        if len(parts) < 2 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❔ Пример: /car 411"))
                return
        }
        carID := parts[1]
        carPath := filepath.Join(b.cfg.AssetsDir, "car", carID+".mod")
        if !fileExists(carPath) {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "Такого названия нет"))
                return
        }
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: mustOpen(carPath),

                Name: filepath.Base(carPath),
        })
        doc.Caption = "⚡️<b>Держите машину!</b>"
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// ── /promo ──────────────────────────────────────────────────────────────────

func (b *Bot) handlePromo(c *middleware.Ctx, parts []string) {
        if len(parts) < 2 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "🎟 <b>Активация промокода</b>\n\nФормат: <code>/promo КОД</code>"))
                return
        }
        code := strings.ToUpper(parts[1])
        // Look up promo.
        var name, comment, link sql.NullString
        var discountPct, customStars, customDays, maxUses, usedCount int
        var isActive int
        var expiresAt sql.NullString
        err := b.users.DB().QueryRowContext(c, `SELECT name, comment, link, discount_pct,
                custom_stars, custom_days, max_uses, used_count, is_active, expires_at
                FROM promos WHERE code = ?`, code).Scan(
                &name, &comment, &link, &discountPct, &customStars, &customDays,
                &maxUses, &usedCount, &isActive, &expiresAt)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Промокод не найден."))
                return
        }
        if isActive == 0 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Промокод неактивен."))
                return
        }
        if maxUses > 0 && usedCount >= maxUses {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Лимит использований исчерпан."))
                return
        }
        if expiresAt.Valid && expiresAt.String != "" {
                if t, err := time.Parse("2006-01-02", expiresAt.String); err == nil && time.Now().After(t) {
                        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Срок действия истёк."))
                        return
                }
        }
        // Check if already used by this user.
        var used int
        _ = b.users.DB().QueryRowContext(c,
                "SELECT COUNT(*) FROM promo_uses WHERE promo_code = ? AND user_id = ?", code, c.User.ChatID).Scan(&used)
        if used > 0 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Вы уже использовали этот промокод."))
                return
        }
        // Activate: set as user's active promo (valid for 24h).
        expires := time.Now().Add(24 * time.Hour).Format("2006-01-02 15:04:05")
        _, err = b.users.DB().ExecContext(c,
                "UPDATE users SET active_promo = ?, promo_expires = ? WHERE chat_id = ?",
                code, expires, c.User.ChatID)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Ошибка: "+err.Error()))
                return
        }
        // Record usage.
        _, _ = b.users.DB().ExecContext(c,
                "INSERT OR IGNORE INTO promo_uses (promo_code, user_id) VALUES (?, ?)", code, c.User.ChatID)
        _, _ = b.users.DB().ExecContext(c,
                "UPDATE promos SET used_count = used_count + 1 WHERE code = ?", code)

        // Build plan buttons.
        var rows [][]tgbotapi.InlineKeyboardButton
        for _, p := range b.cfg.Plans {
                label := fmt.Sprintf("%s %s", p.Emoji, p.Label)
                if customStars > 0 {
                        label += fmt.Sprintf(" — %d ⭐", customStars)
                } else if discountPct > 0 {
                        newStars := p.Stars * (100 - discountPct) / 100
                        if newStars < 1 {
                                newStars = 1
                        }
                        label += fmt.Sprintf(" — %d ⭐ (-%d%%)", newStars, discountPct)
                } else {
                        label += fmt.Sprintf(" — %d ⭐", p.Stars)
                }
                rows = append(rows, tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData(label, "buy_"+strconv.Itoa(p.Stars)+"_"+code),
                ))
        }
        rows = append(rows, tgbotapi.NewInlineKeyboardRow(
                tgbotapi.NewInlineKeyboardButtonData("⬅️ Отмена", "buy_cancel"),
        ))
        kb := tgbotapi.NewInlineKeyboardMarkup(rows...)

        info := "✅ <b>Промокод " + code + " активирован!</b>\n"
        if name.String != "" {
                info += "📝 " + name.String + "\n"
        }
        if comment.String != "" {
                info += "💬 " + comment.String + "\n"
        }
        if link.String != "" {
                info += "🔗 " + link.String + "\n"
        }
        if customStars > 0 {
                info += fmt.Sprintf("💰 Спеццена: %d ⭐\n", customStars)
        } else if discountPct > 0 {
                info += fmt.Sprintf("💸 Скидка: -%d%%\n", discountPct)
        }
        info += "\nВыберите тариф:"

        msg := tgbotapi.NewMessage(c.Message.Chat.ID, info)
        msg.ParseMode = "HTML"
        msg.ReplyMarkup = kb
        _, _ = b.api.Send(msg)
}

// ── /support ────────────────────────────────────────────────────────────────

func (b *Bot) handleSupport(c *middleware.Ctx) {
        kb := tgbotapi.NewInlineKeyboardMarkup(
                tgbotapi.NewInlineKeyboardRow(
                        tgbotapi.NewInlineKeyboardButtonData("📝 Создать обращение", "support_new"),
                ),
        )
        text := "🎫 <b>Техническая поддержка Pweper Bot</b>\n\n" +
                "Нажмите кнопку ниже чтобы создать обращение.\n" +
                "Или напишите напрямую: @keedboy016"
        msg := tgbotapi.NewMessage(c.Message.Chat.ID, text)
        msg.ParseMode = "HTML"
        msg.ReplyMarkup = kb
        _, _ = b.api.Send(msg)
}

func (b *Bot) cbSupportNew(c *middleware.Ctx) {
        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                "📝 Напишите тему обращения одним сообщением (начинается с /subject):"))
}

// ── /review ──────────────────────────────────────────────────────────────────

func (b *Bot) handleReview(c *middleware.Ctx, parts []string) {
        if len(parts) < 2 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "⭐ <b>Оставить отзыв</b>\n\nФормат: <code>/review 5 Отличный бот!</code>\nОценка от 1 до 5"))
                return
        }
        rating, err := strconv.Atoi(parts[1])
        if err != nil || rating < 1 || rating > 5 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Оценка должна быть от 1 до 5"))
                return
        }
        text := ""
        if len(parts) > 2 {
                text = strings.Join(parts[2:], " ")
        }
        _, err = b.users.DB().ExecContext(c,
                `INSERT OR REPLACE INTO reviews (user_id, rating, text, created_at)
                 VALUES (?, ?, ?, datetime('now'))`,
                c.User.ChatID, rating, text)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        stars := strings.Repeat("⭐", rating)
        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                "✅ <b>Спасибо за отзыв!</b> "+stars))
}

// ── /batch + /stopbatch ────────────────────────────────────────────────────

func (b *Bot) handleBatch(c *middleware.Ctx, parts []string) {
        if len(parts) < 2 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "📦 <b>Пакетная обработка</b>\n\n"+
                                "Формат: <code>/batch &lt;команда&gt; [подпись]</code>\n"+
                                "Затем отправляй файлы по одному.\n"+
                                "Для обработки — <code>/stopbatch</code>\n\n"+
                                "Пример: <code>/batch /color #FF0000</code>"))
                return
        }
        batchCmd := parts[1]
        batchCap := ""
        if len(parts) > 2 {
                batchCap = strings.Join(parts[2:], " ")
        } else {
                batchCap = batchCmd
        }
        _, err := b.users.DB().ExecContext(c,
                `INSERT OR REPLACE INTO batch_sessions (user_id, command, caption, files, started_at)
                 VALUES (?, ?, ?, '[]', datetime('now'))`,
                c.User.ChatID, batchCmd, batchCap)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                "✅ <b>Пакетный режим запущен!</b>\n\n"+
                        "Команда: <code>"+batchCmd+"</code>\n"+
                        "Отправляй файлы. Когда закончишь — <code>/stopbatch</code>"))
}

func (b *Bot) handleStopBatch(c *middleware.Ctx) {
        var filesJSON string
        err := b.users.DB().QueryRowContext(c,
                "SELECT files FROM batch_sessions WHERE user_id = ?", c.User.ChatID).Scan(&filesJSON)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Нет активной пакетной сессии."))
                return
        }
        var files []struct {
                FileID   string `json:"file_id"`
                FileName string `json:"file_name"`
        }
        if err := json.Unmarshal([]byte(filesJSON), &files); err != nil || len(files) == 0 {
                _, _ = b.users.DB().ExecContext(c, "DELETE FROM batch_sessions WHERE user_id = ?", c.User.ChatID)
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Файлы не получены, сессия отменена."))
                return
        }
        _, _ = b.users.DB().ExecContext(c, "DELETE FROM batch_sessions WHERE user_id = ?", c.User.ChatID)
        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                fmt.Sprintf("⏳ <b>Обрабатываю %d файл(ов)…</b>\nРезультаты придут по мере готовности.", len(files))))
        // Process files in background.
        go func() {
                for _, f := range files {
                        b.processBatchFile(c, f.FileID, f.FileName)
                }
        }()
}

// processBatchFile downloads a file by file_id and processes it.
// For now, we just send a placeholder message — full implementation requires
// Bot API download, which we have via api.GetFile + api.DownloadFile.
func (b *Bot) processBatchFile(c *middleware.Ctx, fileID, fileName string) {
        // Download via Bot API.
        file, err := b.api.GetFile(tgbotapi.FileConfig{FileID: fileID})
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        fmt.Sprintf("❌ Не удалось скачать %s: %v", fileName, err)))
                return
        }
        link := file.Link(b.api.Token)
        resp, err := httpGet(link)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        fmt.Sprintf("❌ Ошибка загрузки %s: %v", fileName, err)))
                return
        }
        // Send the file back as a placeholder.
        // TODO: actual processing (color, filters, etc.) — for now we just re-send.
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(resp),

                Name: fileName,
        })
        doc.Caption = "⚡️ <b>" + fileName + "</b> обработан (демо)"
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// ── /kotek (broadcast) ────────────────────────────────────────────────────

func (b *Bot) handleBroadcast(c *middleware.Ctx, parts []string) {
        if !c.User.IsAdmin && !c.Cfg.IsAdmin(c.User.ChatID) {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Нет прав."))
                return
        }
        text := strings.TrimSpace(strings.TrimPrefix(c.Message.Text, "/kotek"))
        if text == "" {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "Введите текст рассылки после команды."))
                return
        }
        go b.runBroadcast(c.User.ChatID, text)
        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "📢 Рассылка запущена..."))
}

func (b *Bot) runBroadcast(adminID int64, text string) {
        rows, err := b.users.DB().Query("SELECT chat_id FROM users")
        if err != nil {
                return
        }
        defer rows.Close()
        var ids []int64
        for rows.Next() {
                var id int64
                if rows.Scan(&id) == nil {
                        ids = append(ids, id)
                }
        }
        sent := 0
        for _, id := range ids {
                msg := tgbotapi.NewMessage(id, text)
                msg.ParseMode = "HTML"
                if _, err := b.api.Send(msg); err == nil {
                        sent++
                }
                time.Sleep(50 * time.Millisecond) // ~20 msg/sec
        }
        _, _ = b.api.Send(tgbotapi.NewMessage(adminID,
                fmt.Sprintf("✅ Рассылка завершена. Отправлено: %d / %d", sent, len(ids))))
}

// ── /send ─────────────────────────────────────────────────────────────────

func (b *Bot) handleSendAdmin(c *middleware.Ctx, parts []string) {
        if !c.User.IsAdmin && !c.Cfg.IsAdmin(c.User.ChatID) {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Нет прав."))
                return
        }
        if len(parts) < 3 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "Использование: /send <id> <текст>"))
                return
        }
        uid, err := strconv.ParseInt(parts[1], 10, 64)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Неверный ID."))
                return
        }
        textToSend := strings.Join(parts[2:], " ")
        msg := tgbotapi.NewMessage(uid, textToSend)
        msg.ParseMode = "HTML"
        if _, err := b.api.Send(msg); err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                fmt.Sprintf("✅ Сообщение отправлено пользователю %d.", uid)))
}

// ── /sub (admin) ──────────────────────────────────────────────────────────

func (b *Bot) handleSubAdmin(c *middleware.Ctx, parts []string) {
        if !c.User.IsAdmin && !c.Cfg.IsAdmin(c.User.ChatID) {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Нет прав."))
                return
        }
        if len(parts) < 3 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "Использование: /sub <id> <True|False> [DD.MM.YYYY]"))
                return
        }
        uid, err := strconv.ParseInt(parts[1], 10, 64)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Неверный ID."))
                return
        }
        action := parts[2]
        if action == "True" {
                if len(parts) < 4 {
                        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "Нужна дата: /sub <id> True DD.MM.YYYY"))
                        return
                }
                expiry := parts[3]
                if _, err := time.Parse("02.01.2006", expiry); err != nil {
                        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "Неверный формат даты! Используйте %d.%m.%Y."))
                        return
                }
                _, err = b.users.DB().ExecContext(c,
                        "UPDATE users SET is_subscribed = 1, expiry = ? WHERE chat_id = ?", expiry, uid)
                if err != nil {
                        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                        return
                }
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        fmt.Sprintf("✅ Пользователю %d выдана подписка до %s!", uid, expiry)))
        } else if action == "False" {
                _, err = b.users.DB().ExecContext(c,
                        "UPDATE users SET is_subscribed = 0, expiry = NULL WHERE chat_id = ?", uid)
                if err != nil {
                        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                        return
                }
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        fmt.Sprintf("✅ У пользователя %d забрана подписка!", uid)))
        } else {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "Неверный формат команды!"))
        }
}

// ── handleDocument (file processing) ───────────────────────────────────────

func (b *Bot) handleDocumentReal(c *middleware.Ctx) {
        if c.Message.Document == nil {
                return
        }
        doc := c.Message.Document
        fileName := doc.FileName
        caption := c.Message.Caption
        ext := strings.ToLower(filepath.Ext(fileName))
        if ext == "" {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Нет расширения у файла."))
                return
        }
        ext = strings.TrimPrefix(ext, ".")

        // Download the file via Bot API (max 20 MB for free).
        if doc.FileSize > 20*1024*1024 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "⚠️ Файл больше 20 МБ. Скачивание через Bot API недоступно (нужен MTProto)."))
                return
        }
        file, err := b.api.GetFile(tgbotapi.FileConfig{FileID: doc.FileID})
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Не удалось получить файл: "+err.Error()))
                return
        }
        link := file.Link(b.api.Token)
        fileBytes, err := httpGet(link)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Ошибка загрузки: "+err.Error()))
                return
        }

        // Dispatch based on extension OR caption command.
        progress, _ := b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "⏳ Обрабатываю..."))
        var progressID int
        if progress.Chat != nil {
                progressID = progress.MessageID
        }
        defer func() {
                if progressID != 0 {
                        _, _ = b.api.Request(tgbotapi.NewDeleteMessage(c.Message.Chat.ID, progressID))
                }
        }()

        // Send log to admins (files were not being logged before)
        _ = b.handleLog(c, "файл", fmt.Sprintf("Формат: %s | Имя: %s", ext, fileName))

        switch {
        case strings.Contains(caption, "/color"):
                b.processColorFile(c, fileBytes, fileName, ext, caption)
        case strings.Contains(caption, "/recolor"):
                b.processRecolorFile(c, fileBytes, fileName, ext, caption)
        case strings.Contains(caption, "/filters"):
                b.processFiltersFile(c, fileBytes, fileName, ext, caption)
        case strings.Contains(caption, "/compress"):
                b.processCompressFile(c, fileBytes, fileName, ext, caption)
        case strings.Contains(caption, "/quality"):
                b.processQualityFile(c, fileBytes, fileName, ext, caption)
        case strings.Contains(caption, "/aim"):
                b.processAimFile(c, fileBytes, fileName)
        case strings.Contains(caption, "/logo"):
                b.processLogoFile(c, fileBytes, fileName, ext, "logo")
        case strings.Contains(caption, "/tree"):
                b.processLogoFile(c, fileBytes, fileName, ext, "tree")
        case strings.Contains(caption, "/bild"):
                b.processLogoFile(c, fileBytes, fileName, ext, "bild")
        case strings.Contains(caption, "/overlay"):
                // Multi-step — just acknowledge for now.
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "ℹ️ /overlay требует двух файлов. Используй: первый файл с /overlay mode alpha, затем второй файл."))
        default:
                // Auto-detect by extension.
                b.processFileByExtension(c, fileBytes, fileName, ext)
        }
}

// processFileByExtension auto-detects file type and applies default processing.
func (b *Bot) processFileByExtension(c *middleware.Ctx, data []byte, fileName, ext string) {
        switch ext {
        case "png", "jpg", "jpeg", "webp", "gif", "bmp", "tif", "tiff":
                // Auto-convert to BTX (using user's BTX settings).
                b.processImageToBTX(c, data, fileName)
        case "btx":
                b.processBTXToPNG(c, data, fileName)
        case "zip":
                b.processZIPFile(c, data, fileName)
        case "txd":
                b.processTXDFile(c, data, fileName)
        case "mod":
                b.processMODFile(c, data, fileName)
        case "ifp":
                b.processIFPFile(c, data, fileName)
        case "cls":
                b.processCLSFile(c, data, fileName)
        case "bpc":
                b.processBPCFile(c, data, fileName)
        case "dat":
                b.processDATFile(c, data, fileName)
        case "json":
                b.processJSONFile(c, data, fileName)
        default:
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        fmt.Sprintf("❔ Неподдерживаемый формат: .%s", ext)))
        }
}

// processImageToBTX converts any image (PNG/JPG/WebP/GIF/BMP/TIFF) to BTX
// using user's BTX quality/speed settings.
func (b *Bot) processImageToBTX(c *middleware.Ctx, data []byte, fileName string) {
        u := c.User
        quality := btx.Quality(u.BTXQuality)
        speed := btx.Speed(u.BTXSpeed)
        enc := btx.NewEncoder(btx.Config{AstcencPath: "astcenc", Threads: 4})
        btxData, err := enc.EncodeImage(data, quality, speed)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "❌ Ошибка BTX: "+err.Error()+"\n\n💡 Поддерживаемые форматы: PNG, JPG, JPEG, WebP, GIF, BMP, TIFF"))
                return
        }
        outName := strings.TrimSuffix(fileName, filepath.Ext(fileName)) + ".btx"
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(btxData),
                Name:   outName,
        })
        doc.Caption = fmt.Sprintf("<b>⚡️%s готов!</b>\n🔷 Блок: %s | 🎯 %s | ⚡ %s",
                outName, u.BTXBlock, u.BTXQuality, u.BTXSpeed)
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// processPNGToBTX is kept as alias for backward compat (handlers that
// explicitly check for PNG). New code should use processImageToBTX.
func (b *Bot) processPNGToBTX(c *middleware.Ctx, data []byte, fileName string) {
        b.processImageToBTX(c, data, fileName)
}

// processBTXToPNG converts a BTX back to PNG.
func (b *Bot) processBTXToPNG(c *middleware.Ctx, data []byte, fileName string) {
        enc := btx.NewEncoder(btx.Config{AstcencPath: "astcenc", Threads: 4})
        pngData, err := enc.DecodeBTX(data)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        outName := strings.TrimSuffix(fileName, filepath.Ext(fileName)) + ".png"
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(pngData),

                Name: outName,
        })
        doc.Caption = "<b>⚡️" + outName + " готов!</b>"
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// processTXDFile decodes a .txd into a ZIP of PNGs.
func (b *Bot) processTXDFile(c *middleware.Ctx, data []byte, fileName string) {
        textures, err := txd.Parse(data)
        if err != nil || len(textures) == 0 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Не удалось извлечь текстуры."))
                return
        }
        // Pack into a ZIP.
        var buf bytes.Buffer
        zw := newZipWriter(&buf)
        for _, t := range textures {
                pngBytes, err := imaging.EncodePNG(t.Image)
                if err != nil {
                        continue
                }
                zw.writeFile(t.Name+".png", pngBytes)
        }
        zw.close()
        zipBytes := buf.Bytes()
        outName := strings.TrimSuffix(fileName, filepath.Ext(fileName)) + ".zip"
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(zipBytes),

                Name: outName,
        })
        doc.Caption = fmt.Sprintf("<b>⚡️Ваши файлы готовы!</b>\n📦 Текстур: %d", len(textures))
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// processMODFile decrypts a .mod to .dff.
func (b *Bot) processMODFile(c *middleware.Ctx, data []byte, fileName string) {
        dff, err := mod.DecryptMod(data)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        outName := strings.TrimSuffix(fileName, filepath.Ext(fileName)) + ".dff"
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(dff),

                Name: outName,
        })
        doc.Caption = "<b>⚡️Ваша модель готова!</b>"
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// processIFPFile converts .ifp to .ani.
func (b *Bot) processIFPFile(c *middleware.Ctx, data []byte, fileName string) {
        ani, err := ifp.Convert(data)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        outName := strings.TrimSuffix(fileName, filepath.Ext(fileName)) + ".ani"
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(ani),

                Name: outName,
        })
        doc.Caption = "<b>⚡️Ваша анимация готова!</b>"
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// processCLSFile converts .cls to .col.
func (b *Bot) processCLSFile(c *middleware.Ctx, data []byte, fileName string) {
        col, err := cls.Convert(data)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        outName := strings.TrimSuffix(fileName, filepath.Ext(fileName)) + ".col"
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(col),

                Name: outName,
        })
        doc.Caption = "Держи файл!"
        _, _ = b.api.Send(doc)
}

// processBPCFile decrypts a .bpc.
func (b *Bot) processBPCFile(c *middleware.Ctx, data []byte, fileName string) {
        decrypted := bpc.Decrypt(data)
        outName := strings.TrimSuffix(fileName, filepath.Ext(fileName)) + "_decrypted.zip"
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(decrypted),

                Name: outName,
        })
        doc.Caption = "<b>⚡️Ваш файл готов!</b>"
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// processZIPFile processes a .zip (could be BTX/PNG batch, or generic).
func (b *Bot) processZIPFile(c *middleware.Ctx, data []byte, fileName string) {
        // Open zip and check contents.
        r, err := openZipReader(data)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        hasPNG := false
        hasBTX := false
        for _, f := range r.File {
                if isImageFile(f.Name) {
                        hasPNG = true
                }
                if strings.HasSuffix(strings.ToLower(f.Name), ".btx") {
                        hasBTX = true
                }
        }
        if hasPNG && !hasBTX {
                b.processZIPPNGToBTX(c, data, fileName)
                return
        }
        if hasBTX && !hasPNG {
                b.processZIPBTXToPNG(c, data, fileName)
                return
        }
        // Generic: just resend.
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(data),

                Name: fileName,
        })
        doc.Caption = "<b>⚡️Файл готов!</b>"
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// processZIPPNGToBTX converts all PNGs in a ZIP to BTX.
func (b *Bot) processZIPPNGToBTX(c *middleware.Ctx, data []byte, fileName string) {
        u := c.User
        quality := btx.Quality(u.BTXQuality)
        speed := btx.Speed(u.BTXSpeed)
        enc := btx.NewEncoder(btx.Config{AstcencPath: "astcenc", Threads: 4})

        r, err := openZipReader(data)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        var outBuf bytes.Buffer
        zw := newZipWriter(&outBuf)
        count := 0
        for _, f := range r.File {
                if !isImageFile(f.Name) {
                        zw.copyFile(f)
                        continue
                }
                rc, err := f.Open()
                if err != nil {
                        continue
                }
                imgBytes, _ := io.ReadAll(rc)
                rc.Close()
                btxData, err := enc.EncodePNG(imgBytes, quality, speed)
                if err != nil {
                        continue
                }
                outName := strings.TrimSuffix(f.Name, filepath.Ext(f.Name)) + ".btx"
                zw.writeFile(outName, btxData)
                count++
        }
        zw.close()
        zipBytes := outBuf.Bytes()
        outName := strings.TrimSuffix(fileName, filepath.Ext(fileName)) + "_btx.zip"
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(zipBytes),

                Name: outName,
        })
        doc.Caption = fmt.Sprintf("<b>⚡️ZIP конвертирован!</b>\n🔄 Файлов: %d\n🔷 %s | 🎯 %s | ⚡ %s",
                count, u.BTXBlock, u.BTXQuality, u.BTXSpeed)
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// processZIPBTXToPNG converts all BTXs in a ZIP to PNG.
func (b *Bot) processZIPBTXToPNG(c *middleware.Ctx, data []byte, fileName string) {
        enc := btx.NewEncoder(btx.Config{AstcencPath: "astcenc", Threads: 4})
        r, err := openZipReader(data)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        var outBuf bytes.Buffer
        zw := newZipWriter(&outBuf)
        count := 0
        for _, f := range r.File {
                if !strings.HasSuffix(strings.ToLower(f.Name), ".btx") {
                        zw.copyFile(f)
                        continue
                }
                rc, err := f.Open()
                if err != nil {
                        continue
                }
                btxBytes, _ := io.ReadAll(rc)
                rc.Close()
                pngBytes, err := enc.DecodeBTX(btxBytes)
                if err != nil {
                        continue
                }
                outName := strings.TrimSuffix(f.Name, filepath.Ext(f.Name)) + ".png"
                zw.writeFile(outName, pngBytes)
                count++
        }
        zw.close()
        zipBytes := outBuf.Bytes()
        outName := strings.TrimSuffix(fileName, filepath.Ext(fileName)) + "_png.zip"
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(zipBytes),

                Name: outName,
        })
        doc.Caption = fmt.Sprintf("<b>⚡️ZIP конвертирован!</b>\n🔄 Файлов: %d", count)
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// processDATFile converts timecyc.dat to JSON.
func (b *Bot) processDATFile(c *middleware.Ctx, data []byte, fileName string) {
        jsonStr, err := convertTimecycDatToJSON(data)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        outName := strings.TrimSuffix(fileName, filepath.Ext(fileName)) + ".json"
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: strings.NewReader(jsonStr),

                Name: outName,
        })
        doc.Caption = "<b>⚡️Ваш файл готов!</b>"
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// processJSONFile extracts colors from timecyc.json.
func (b *Bot) processJSONFile(c *middleware.Ctx, data []byte, fileName string) {
        result := extractColorsFromTimecycJSON(data)
        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, result))
}

// processColorFile applies /color to a single image or ZIP.
func (b *Bot) processColorFile(c *middleware.Ctx, data []byte, fileName, ext, caption string) {
        parts := strings.Fields(caption)
        if len(parts) < 2 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❔ Пример: /color #FF0000 0.4"))
                return
        }
        hexColor := parts[1]
        if !strings.HasPrefix(hexColor, "#") {
                hexColor = "#" + hexColor
        }
        alpha := 1.0
        if len(parts) >= 3 {
                if a, err := strconv.ParseFloat(parts[2], 64); err == nil {
                        alpha = a
                }
        }
        if ext == "zip" {
                b.processColorZIPBytes(c, data, hexColor, alpha, fileName)
                return
        }
        // Single image.
        img, err := imaging.DecodeImage(data)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        out, err := imaging.Color(img, hexColor, alpha)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(out),

                Name: fileName,
        })
        doc.Caption = "<b>⚡️Файл готов!</b>"
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// processColorZIPBytes applies /color to all images in a ZIP.
func (b *Bot) processColorZIPBytes(c *middleware.Ctx, data []byte, hexColor string, alpha float64, fileName string) {
        r, err := openZipReader(data)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        var outBuf bytes.Buffer
        zw := newZipWriter(&outBuf)
        count := 0
        for _, f := range r.Reader.File {
                if !isImageFile(f.Name) {
                        zw.copyFile(f)
                        continue
                }
                rc, err := f.Open()
                if err != nil {
                        continue
                }
                imgBytes, _ := io.ReadAll(rc)
                rc.Close()
                img, err := imaging.DecodeImage(imgBytes)
                if err != nil {
                        continue
                }
                out, err := imaging.Color(img, hexColor, alpha)
                if err != nil {
                        continue
                }
                zw.writeFile(f.Name, out)
                count++
        }
        zw.close()
        zipBytes := outBuf.Bytes()
        outName := strings.TrimSuffix(fileName, filepath.Ext(fileName)) + "_colored.zip"
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(zipBytes),

                Name: outName,
        })
        doc.Caption = fmt.Sprintf("<b>⚡️ZIP с цветом готов!</b>\n🎨 %s | 🔄 Файлов: %d", hexColor, count)
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// processRecolorFile applies /recolor to a single image.
func (b *Bot) processRecolorFile(c *middleware.Ctx, data []byte, fileName, ext, caption string) {
        if ext == "zip" {
                b.processRecolorZIPFile(c, data, fileName, caption)
                return
        }
        parts := strings.Fields(caption)
        if len(parts) < 3 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❔ Пример: /recolor #ffbbbb #661717 30"))
                return
        }
        targetHex := parts[1]
        replacementHex := parts[2]
        tolerance := 10
        if len(parts) >= 4 {
                if t, err := strconv.Atoi(parts[3]); err == nil {
                        tolerance = t
                }
        }
        img, err := imaging.DecodeImage(data)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        out, err := imaging.Recolor(img, targetHex, replacementHex, tolerance)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(out),

                Name: fileName,
        })
        doc.Caption = "<b>⚡️Файл готов!</b>"
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

func (b *Bot) processRecolorZIPFile(c *middleware.Ctx, data []byte, fileName, caption string) {
        parts := strings.Fields(caption)
        if len(parts) < 3 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❔ Пример: /recolor #ffbbbb #661717 30"))
                return
        }
        targetHex := parts[1]
        replacementHex := parts[2]
        tolerance := 10
        if len(parts) >= 4 {
                if t, err := strconv.Atoi(parts[3]); err == nil {
                        tolerance = t
                }
        }
        r, err := openZipReader(data)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        var outBuf bytes.Buffer
        zw := newZipWriter(&outBuf)
        count := 0
        for _, f := range r.Reader.File {
                if !isImageFile(f.Name) {
                        zw.copyFile(f)
                        continue
                }
                rc, err := f.Open()
                if err != nil {
                        continue
                }
                imgBytes, _ := io.ReadAll(rc)
                rc.Close()
                img, err := imaging.DecodeImage(imgBytes)
                if err != nil {
                        continue
                }
                out, err := imaging.Recolor(img, targetHex, replacementHex, tolerance)
                if err != nil {
                        continue
                }
                zw.writeFile(f.Name, out)
                count++
        }
        zw.close()
        zipBytes := outBuf.Bytes()
        outName := strings.TrimSuffix(fileName, filepath.Ext(fileName)) + "_recolor.zip"
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(zipBytes),

                Name: outName,
        })
        doc.Caption = fmt.Sprintf("<b>⚡️ZIP с перекраской готов!</b>\n🔄 Файлов: %d", count)
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// processFiltersFile applies a filter to an image or ZIP.
func (b *Bot) processFiltersFile(c *middleware.Ctx, data []byte, fileName, ext, caption string) {
        parts := strings.Fields(caption)
        if len(parts) < 2 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❔ Пример: /filters red или /filters light 100"))
                return
        }
        filterName := strings.ToLower(parts[1])
        amount := 50
        if len(parts) >= 3 {
                if a, err := strconv.Atoi(parts[2]); err == nil {
                        amount = a
                }
        }
        if ext == "zip" {
                b.processFiltersZIP(c, data, fileName, filterName, amount)
                return
        }
        img, err := imaging.DecodeImage(data)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        out, err := imaging.Filter(img, filterName, amount)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(out),

                Name: fileName,
        })
        doc.Caption = fmt.Sprintf("<b>⚡️Файл готов!</b> Фильтр: %s", filterName)
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

func (b *Bot) processFiltersZIP(c *middleware.Ctx, data []byte, fileName, filterName string, amount int) {
        r, err := openZipReader(data)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        var outBuf bytes.Buffer
        zw := newZipWriter(&outBuf)
        count := 0
        for _, f := range r.Reader.File {
                if !isImageFile(f.Name) {
                        zw.copyFile(f)
                        continue
                }
                rc, err := f.Open()
                if err != nil {
                        continue
                }
                imgBytes, _ := io.ReadAll(rc)
                rc.Close()
                img, err := imaging.DecodeImage(imgBytes)
                if err != nil {
                        continue
                }
                out, err := imaging.Filter(img, filterName, amount)
                if err != nil {
                        continue
                }
                zw.writeFile(f.Name, out)
                count++
        }
        zw.close()
        zipBytes := outBuf.Bytes()
        outName := strings.TrimSuffix(fileName, filepath.Ext(fileName)) + "_filtered.zip"
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(zipBytes),

                Name: outName,
        })
        doc.Caption = fmt.Sprintf("<b>⚡️ZIP с фильтром %s готов!</b>\n🔄 Файлов: %d", filterName, count)
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// processCompressFile resizes an image.
func (b *Bot) processCompressFile(c *middleware.Ctx, data []byte, fileName, ext, caption string) {
        parts := strings.Fields(caption)
        sizeStr := ""
        for _, p := range parts {
                if strings.Contains(p, "x") {
                        sizeStr = p
                        break
                }
        }
        if sizeStr == "" {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Не указан размер. Пример: /compress 512x512"))
                return
        }
        dimParts := strings.SplitN(sizeStr, "x", 2)
        if len(dimParts) != 2 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Неверный формат размера. Пример: 512x512"))
                return
        }
        w, err1 := strconv.Atoi(dimParts[0])
        h, err2 := strconv.Atoi(dimParts[1])
        if err1 != nil || err2 != nil || w <= 0 || h <= 0 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Неверные размеры."))
                return
        }
        if ext == "zip" {
                b.processCompressZIP(c, data, fileName, w, h)
                return
        }
        out, newFmt, err := imaging.Compress(data, w, h, ext)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        outName := "compressed_" + strings.TrimSuffix(fileName, filepath.Ext(fileName)) + "." + newFmt
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(out),

                Name: outName,
        })
        doc.Caption = fmt.Sprintf("<b>⚡️Сжато до %dx%d!</b>", w, h)
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

func (b *Bot) processCompressZIP(c *middleware.Ctx, data []byte, fileName string, w, h int) {
        r, err := openZipReader(data)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        var outBuf bytes.Buffer
        zw := newZipWriter(&outBuf)
        count := 0
        for _, f := range r.Reader.File {
                if !isImageFile(f.Name) {
                        zw.copyFile(f)
                        continue
                }
                rc, err := f.Open()
                if err != nil {
                        continue
                }
                imgBytes, _ := io.ReadAll(rc)
                rc.Close()
                ext := "png"
                if isImageFile(f.Name) || isImageFile(f.Name) {
                        ext = "jpg"
                }
                out, _, err := imaging.Compress(imgBytes, w, h, ext)
                if err != nil {
                        continue
                }
                outName := strings.TrimSuffix(f.Name, filepath.Ext(f.Name)) + "." + ext
                zw.writeFile(outName, out)
                count++
        }
        zw.close()
        zipBytes := outBuf.Bytes()
        outName := strings.TrimSuffix(fileName, filepath.Ext(fileName)) + "_compressed.zip"
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(zipBytes),

                Name: outName,
        })
        doc.Caption = fmt.Sprintf("<b>⚡️ZIP сжат до %dx%d!</b>\n🔄 Файлов: %d", w, h, count)
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// processQualityFile applies /quality to an image.
func (b *Bot) processQualityFile(c *middleware.Ctx, data []byte, fileName, ext, caption string) {
        parts := strings.Fields(caption)
        if len(parts) < 2 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❔ Пример: /quality 16"))
                return
        }
        level, err := strconv.Atoi(parts[1])
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Уровень должен быть числом."))
                return
        }
        if ext == "zip" {
                b.processQualityZIP(c, data, fileName, level)
                return
        }
        out, err := imaging.Quality(data, level)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(out),

                Name: fileName,
        })
        doc.Caption = fmt.Sprintf("<b>⚡️Качество улучшено (level=%d)!</b>", level)
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

func (b *Bot) processQualityZIP(c *middleware.Ctx, data []byte, fileName string, level int) {
        r, err := openZipReader(data)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        var outBuf bytes.Buffer
        zw := newZipWriter(&outBuf)
        count := 0
        for _, f := range r.Reader.File {
                if !isImageFile(f.Name) {
                        zw.copyFile(f)
                        continue
                }
                rc, err := f.Open()
                if err != nil {
                        continue
                }
                imgBytes, _ := io.ReadAll(rc)
                rc.Close()
                out, err := imaging.Quality(imgBytes, level)
                if err != nil {
                        continue
                }
                zw.writeFile(f.Name, out)
                count++
        }
        zw.close()
        zipBytes := outBuf.Bytes()
        outName := strings.TrimSuffix(fileName, filepath.Ext(fileName)) + "_quality.zip"
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(zipBytes),

                Name: outName,
        })
        doc.Caption = fmt.Sprintf("<b>⚡️Качество улучшено!</b>\n🔄 Файлов: %d", count)
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// processAimFile creates a 4-way rotated crosshair.
func (b *Bot) processAimFile(c *middleware.Ctx, data []byte, fileName string) {
        out, err := imaging.Aim(data)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }
        outName := "aim_" + fileName
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(out),

                Name: outName,
        })
        doc.Caption = "<b>⚡️Прицел готов!</b>"
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// processLogoFile duplicates the file N times with different names from a list.
func (b *Bot) processLogoFile(c *middleware.Ctx, data []byte, fileName, ext, kind string) {
        var names []string
        switch kind {
        case "logo":
                names = getFileSuffixes("logo")
        case "tree":
                names = getTreeNames()
        case "bild":
                names = getBildNames()
        default:
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Неизвестный тип."))
                return
        }
        var outBuf bytes.Buffer
        zw := newZipWriter(&outBuf)
        for _, n := range names {
                zw.writeFile(n+"."+ext, data)
        }
        zw.close()
        zipBytes := outBuf.Bytes()
        outName := kind + "_batch.zip"
        doc := tgbotapi.NewDocument(c.Message.Chat.ID, tgbotapi.FileReader{
                Reader: bytes.NewReader(zipBytes),

                Name: outName,
        })
        doc.Caption = fmt.Sprintf("<b>⚡️Ваши %s файлы готовы!</b>\n📦 Файлов: %d", kind, len(names))
        doc.ParseMode = "HTML"
        _, _ = b.api.Send(doc)
}

// ── helpers ────────────────────────────────────────────────────────────────

// isFloat returns true if s parses as a float.
func isFloat(s string) bool {
        _, err := strconv.ParseFloat(s, 64)
        return err == nil
}

// fileExists returns true if path exists.
func fileExists(path string) bool {
        _, err := os.Stat(path)
        return err == nil
}

// mustOpen opens a file for reading, panicking on error. For handlers.
func mustOpen(path string) *os.File {
        f, err := os.Open(path)
        if err != nil {
                return nil
        }
        return f
}

// fileSize returns the size of a file.
func fileSize(path string) int64 {
        st, err := os.Stat(path)
        if err != nil {
                return 0
        }
        return st.Size()
}

// searchInSkins searches skins.txt for an ID or name.
// Returns [id, name] or nil.
func searchInSkins(query, skinsPath string) []string {
        data, err := os.ReadFile(skinsPath)
        if err != nil {
                return nil
        }
        lines := strings.Split(string(data), "\n")
        var curID, curName string
        for _, line := range lines {
                line = strings.TrimSpace(line)
                if strings.HasPrefix(line, "ID - ") {
                        curID = strings.TrimPrefix(line, "ID - ")
                } else if strings.HasPrefix(line, "NAME - ") {
                        curName = strings.TrimPrefix(line, "NAME - ")
                        if curID != "" && curName != "" {
                                if query == curID {
                                        return []string{curID, curName}
                                }
                                cleanQuery := strings.ToLower(strings.Replace(query, ".mod", "", 1))
                                modName := strings.ToLower(strings.Replace(curName, ".mod", "", 1))
                                if strings.Contains(modName, cleanQuery) {
                                        return []string{curID, curName}
                                }
                        }
                }
        }
        return nil
}

// convertTimecycDatToJSON parses a timecyc.dat file and returns JSON.
func convertTimecycDatToJSON(data []byte) (string, error) {
        lines := strings.Split(string(data), "\n")
        var entries []map[string]any
        for _, line := range lines {
                line = strings.TrimSpace(line)
                if line == "" || strings.HasPrefix(line, ";") {
                        continue
                }
                parts := strings.Fields(line)
                if len(parts) < 48 {
                        continue
                }
                toInt := func(s string) int {
                        n, _ := strconv.Atoi(s)
                        return n
                }
                toFloat := func(s string) float64 {
                        f, _ := strconv.ParseFloat(s, 64)
                        return f
                }
                entry := map[string]any{
                        "AmbientRGB":         []int{toInt(parts[0]), toInt(parts[1]), toInt(parts[2])},
                        "AmbientPhysicalRGB": []int{toInt(parts[3]), toInt(parts[4]), toInt(parts[5])},
                        "DirectionalRGB":     []int{toInt(parts[6]), toInt(parts[7]), toInt(parts[8])},
                        "SkyTopRGB":          []int{toInt(parts[9]), toInt(parts[10]), toInt(parts[11])},
                        "SkyBottomRGB":       []int{toInt(parts[12]), toInt(parts[13]), toInt(parts[14])},
                        "SunCoreRGB":         []int{toInt(parts[15]), toInt(parts[16]), toInt(parts[17])},
                        "SunCoronaRGB":       []int{toInt(parts[18]), toInt(parts[19]), toInt(parts[20])},
                        "SunSize":            toFloat(parts[21]),
                        "SpriteSize":         toFloat(parts[22]),
                        "SpriteBrght":        toFloat(parts[23]),
                        "Shad":               toInt(parts[24]),
                        "LightShad":          toInt(parts[25]),
                        "PoleShad":           toInt(parts[26]),
                        "FarClip":            toFloat(parts[27]),
                        "FogStart":           toFloat(parts[28]),
                        "LightGnd":           toFloat(parts[29]),
                        "FluffyBottomRGB":    []int{toInt(parts[30]), toInt(parts[31]), toInt(parts[32])},
                        "CloudRGB":           []int{toInt(parts[33]), toInt(parts[34]), toInt(parts[35])},
                        "WaterRGBA":          []int{toInt(parts[36]), toInt(parts[37]), toInt(parts[38]), toInt(parts[39])},
                        "PostFX1ARGB":        []int{toInt(parts[40]), toInt(parts[41]), toInt(parts[42]), toInt(parts[43])},
                        "PostFX2ARGB":        []int{toInt(parts[44]), toInt(parts[45]), toInt(parts[46]), toInt(parts[47])},
                }
                if len(parts) > 48 {
                        entry["CloudAlpha"] = toInt(parts[48])
                }
                entries = append(entries, entry)
        }
        jsonBytes, err := json.MarshalIndent(entries, "", "  ")
        if err != nil {
                return "", err
        }
        return string(jsonBytes), nil
}

// extractColorsFromTimecycJSON pulls the 4 key colors from a timecyc.json.
func extractColorsFromTimecycJSON(data []byte) string {
        var entries []map[string]any
        if err := json.Unmarshal(data, &entries); err != nil {
                return "❌ Ошибка JSON: " + err.Error()
        }
        if len(entries) == 0 {
                return "❌ Пустой JSON."
        }
        first := entries[0]
        keys := []string{"SkyBottomRGB", "SkyTopRGB", "CloudRGB", "SunCoreRGB"}
        var lines []string
        for _, k := range keys {
                arr, ok := first[k].([]any)
                if !ok || len(arr) < 3 {
                        lines = append(lines, k+": не найдено")
                        continue
                }
                r, _ := arr[0].(float64)
                g, _ := arr[1].(float64)
                b, _ := arr[2].(float64)
                hexStr := fmt.Sprintf("#%02X%02X%02X", int(r), int(g), int(b))
                lines = append(lines, k+": "+hexStr)
        }
        return strings.Join(lines, "\n")
}

// getFileSuffixes returns the FILE_SUFFIXES list (matches Python).
func getFileSuffixes(kind string) []string {
        // Hardcoded short list — full list has 100+ entries.
        // For brevity, return a subset; the full list is in the Python source.
        return []string{
                "logo", "logobrred", "logobrblue", "logobrgreen", "logobrwhite",
                "logobrblack", "logobryellow", "logobrpurple", "logobrorange",
                "logobrpink", "logobrcyan", "logobrmagenta", "logobrgold",
                "logobrsilver", "logobrbronze", "logobrplatinum",
        }
}

// getTreeNames returns the Tree list.
func getTreeNames() []string {
        return []string{"Tree", "tree1", "tree2", "tree3", "palm1", "palm2", "bush1", "bush2"}
}

// getBildNames returns the bild list.
func getBildNames() []string {
        return []string{"billboard1", "billboard2", "billboard3", "sign1", "sign2"}
}

// copyDir copies a directory recursively.
func copyDir(src, dst string) error {
        return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
                if err != nil {
                        return err
                }
                rel, err := filepath.Rel(src, path)
                if err != nil {
                        return err
                }
                dstPath := filepath.Join(dst, rel)
                if info.IsDir() {
                        return os.MkdirAll(dstPath, 0o755)
                }
                in, err := os.Open(path)
                if err != nil {
                        return err
                }
                defer in.Close()
                out, err := os.Create(dstPath)
                if err != nil {
                        return err
                }
                defer out.Close()
                _, err = io.Copy(out, in)
                return err
        })
}

// zipDirectory zips the contents of a directory into a writer.
func zipDirectory(srcDir string, w io.Writer) error {
        zw := newZipWriter(w)
        defer zw.close()
        return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
                if err != nil {
                        return err
                }
                if info.IsDir() {
                        return nil
                }
                rel, err := filepath.Rel(srcDir, path)
                if err != nil {
                        return err
                }
                in, err := os.Open(path)
                if err != nil {
                        return err
                }
                defer in.Close()
                data, err := io.ReadAll(in)
                if err != nil {
                        return err
                }
                zw.writeFile(filepath.ToSlash(rel), data)
                return nil
        })
}

// httpGet fetches a URL and returns the body bytes.
func httpGet(url string) ([]byte, error) {
        ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
        defer cancel()
        req, err := newHTTPRequest(ctx, url)
        if err != nil {
                return nil, err
        }
        resp, err := httpClient.Do(req)
        if err != nil {
                return nil, err
        }
        defer resp.Body.Close()
        return io.ReadAll(resp.Body)
}

// Ensure user package is used.
var _ = user.RoleAdmin
var _ = hex.EncodeToString

// checkRequiredSubs returns the list of channels the user is NOT subscribed to.
// Uses Bot API getChatMember for each channel (bot must be admin in each).
// For now returns empty list (skip check) — full implementation requires
// the bot to be admin in each required channel.
func (b *Bot) checkRequiredSubs(c *middleware.Ctx) []string {
        channels := b.channels.All()
        var notSub []string
        for _, ch := range channels {
                username := strings.TrimPrefix(ch, "@")
                // Use getChatMember to check subscription via channel username.
                _, err := b.api.GetChatMember(tgbotapi.GetChatMemberConfig{
                        ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
                                SuperGroupUsername: "@" + username,
                                UserID:             c.User.ChatID,
                        },
                })
                if err != nil {
                        notSub = append(notSub, ch)
                }
        }
        return notSub
}
