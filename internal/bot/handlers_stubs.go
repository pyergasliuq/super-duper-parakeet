// Package bot — handlers_stubs.go
//
// All admin stubs have been replaced with real implementations in
// handlers_admin_real.go. This file now only contains payment handlers
// and small helper functions.
package bot

import (
        "fmt"
        "time"

        tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

        "github.com/pweper/bot/internal/bot/middleware"
)

// notYetImplemented sends a short "coming soon" message and logs the call.
func (b *Bot) notYetImplemented(c *middleware.Ctx, feature string) {
        b.logger.Info("not yet implemented",
                "feature", feature, "user_id", c.User.ChatID)
        msg := tgbotapi.NewMessage(c.Message.Chat.ID,
                fmt.Sprintf("🚧 <b>%s</b> — эта функция ещё не перенесена на Go. Скоро будет доступна.", feature))
        msg.ParseMode = "HTML"
        _, _ = b.api.Send(msg)
}

// ── Payment ────────────────────────────────────────────────────────────────

func (b *Bot) handlePreCheckout(c *middleware.Ctx) {
        if c.Update.PreCheckoutQuery != nil {
                callback := tgbotapi.PreCheckoutConfig{
                        PreCheckoutQueryID: c.Update.PreCheckoutQuery.ID,
                        OK:                 true,
                        ErrorMessage:       "",
                }
                _, _ = b.api.Request(callback)
        }
}

func (b *Bot) handleSuccessfulPayment(c *middleware.Ctx) {
        // Look up the invoice by payload.
        if c.Message.SuccessfulPayment == nil {
                return
        }
        payload := c.Message.SuccessfulPayment.InvoicePayload
        var userID int64
        var stars, days int
        err := b.users.DB().QueryRowContext(c,
                "SELECT user_id, stars, days FROM pending_invoices WHERE payload = ?",
                payload).Scan(&userID, &stars, &days)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "✅ Оплата получена, но возникла ошибка выдачи. Напишите @keedboy016"))
                return
        }
        // Grant subscription.
        expiry, err := b.users.GrantSubscription(c, userID, days)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "❌ Ошибка выдачи подписки: "+err.Error()))
                return
        }
        // Remove the pending invoice.
        _, _ = b.users.DB().ExecContext(c,
                "DELETE FROM pending_invoices WHERE payload = ?", payload)
        // Log the purchase.
        _, _ = b.users.DB().ExecContext(c,
                `INSERT INTO purchases (user_id, stars, days, plan_label, created_at)
                 VALUES (?, ?, ?, ?, datetime('now'))`,
                userID, stars, days, fmt.Sprintf("%d дней", days))

        // Notify the user.
        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                fmt.Sprintf("✅ <b>Оплата получена!</b>\n\n💎 Premium активирован до <b>%s</b>", expiry)))
}

func (b *Bot) cbBuy(c *middleware.Ctx, data string) {
        // data = "buy_<stars>" or "buy_<stars>_<promo_code>"
        parts := splitUnderscores(data)
        if len(parts) < 2 {
                return
        }
        stars, err := parseInt(parts[1])
        if err != nil {
                return
        }
        // Find the plan.
        var plan *struct {
                Stars int
                Days  int
                Label string
                Emoji string
        }
        for _, p := range b.cfg.Plans {
                if p.Stars == stars {
                        plan = &struct {
                                Stars int
                                Days  int
                                Label string
                                Emoji string
                        }{p.Stars, p.Days, p.Label, p.Emoji}
                        break
                }
        }
        if plan == nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ План не найден."))
                return
        }
        // Build invoice payload.
        payload := fmt.Sprintf("sub_%d_%d_%d_%d", stars, plan.Days, c.User.ChatID, time.Now().Unix())

        // Save pending invoice.
        _, err = b.users.DB().ExecContext(c,
                `INSERT OR REPLACE INTO pending_invoices (payload, user_id, stars, days, created_at)
                 VALUES (?, ?, ?, ?, datetime('now'))`,
                payload, c.User.ChatID, stars, plan.Days)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ "+err.Error()))
                return
        }

        // Send invoice.
        title := fmt.Sprintf("%s Premium %s", plan.Emoji, plan.Label)
        description := fmt.Sprintf("Подписка на %s.\n✅ Без ограничений\n✅ Приоритетная очередь", plan.Label)
        invoice := tgbotapi.NewInvoice(c.Message.Chat.ID, title, description, payload, "", "", "XTR",
                []tgbotapi.LabeledPrice{{Label: plan.Label, Amount: stars}})
        // Telegram Stars (XTR) requires empty arrays for tip amounts.
        invoice.SuggestedTipAmounts = []int{}
        _, err = b.api.Send(invoice)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Ошибка счёта: "+err.Error()))
        }
}

func (b *Bot) cbBuyTrial(c *middleware.Ctx) {
        if c.User.TrialUsed {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Trial уже использован."))
                return
        }
        stars := b.cfg.TrialStars
        days := b.cfg.TrialDays
        payload := fmt.Sprintf("trial_%d_%d_%d", stars, days, c.User.ChatID)
        _, _ = b.users.DB().ExecContext(c,
                `INSERT OR REPLACE INTO pending_invoices (payload, user_id, stars, days, created_at)
                 VALUES (?, ?, ?, ?, datetime('now'))`,
                payload, c.User.ChatID, stars, days)
        title := "⚡ Trial Premium"
        description := fmt.Sprintf("Пробная подписка на %d дней.", days)
        invoice := tgbotapi.NewInvoice(c.Message.Chat.ID, title, description, payload, "", "", "XTR",
                []tgbotapi.LabeledPrice{{Label: "Trial", Amount: stars}})
        // Telegram Stars (XTR) requires empty arrays for tip amounts.
        invoice.SuggestedTipAmounts = []int{}
        _, _ = b.api.Send(invoice)
}

func (b *Bot) cbShowPlans(c *middleware.Ctx) {
        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                "💎 <b>Тарифы Premium:</b>"))
        // Reuse kbSubscriptionPlans.
        msg := tgbotapi.NewMessage(c.Message.Chat.ID, b.startFreeText())
        msg.ParseMode = "HTML"
        msg.ReplyMarkup = b.kbSubscriptionPlans()
        _, _ = b.api.Send(msg)
}

func (b *Bot) cbPromoApply(c *middleware.Ctx, data string) {
        // data = "promo_apply_<stars>_<code>"
        // For now, just trigger buy flow.
        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                "ℹ️ Активируйте промокод командой /promo <код> для применения скидки."))
}

func (b *Bot) cbRecheckChannels(c *middleware.Ctx) {
        // Re-check required channel subscriptions.
        notSub := b.checkRequiredSubs(c)
        if len(notSub) == 0 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "✅ Подписка проверена! Теперь можете использовать бота."))
                return
        }
        msg := tgbotapi.NewMessage(c.Message.Chat.ID,
                "🔔 <b>Вы не подписаны на:</b>\n\n"+joinStrings(notSub, "\n"))
        msg.ParseMode = "HTML"
        msg.ReplyMarkup = b.kbCheckChannels(notSub)
        _, _ = b.api.Send(msg)
}

// ── /compress text command (delegates to file handler) ────────────────────

// handleCompressText is the text-only /compress handler — just shows usage.
func (b *Bot) handleCompressText(c *middleware.Ctx) {
        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                "❔ Пример: отправьте файл с подписью <code>/compress 512x512</code>"))
}

// ── small string helpers ───────────────────────────────────────────────────

func splitUnderscores(s string) []string {
        var out []string
        cur := ""
        for _, r := range s {
                if r == '_' {
                        out = append(out, cur)
                        cur = ""
                } else {
                        cur += string(r)
                }
        }
        if cur != "" {
                out = append(out, cur)
        }
        return out
}

func parseInt(s string) (int, error) {
        var n int
        for _, r := range s {
                if r < '0' || r > '9' {
                        return 0, fmt.Errorf("not a number: %s", s)
                }
                n = n*10 + int(r-'0')
        }
        return n, nil
}

func joinStrings(items []string, sep string) string {
        if len(items) == 0 {
                return ""
        }
        out := items[0]
        for _, s := range items[1:] {
                out += sep + s
        }
        return out
}
