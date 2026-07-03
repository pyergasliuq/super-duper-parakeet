// Package bot — handlers_admin.go
//
// Real implementations of the admin panel: dashboard, stats, top users,
// active subscriptions list (with pagination), and user search.
//
// Replaces stubs from handlers_stubs.go.
package bot

import (
        "fmt"
        "strconv"
        "strings"

        tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

        "github.com/pweper/bot/internal/bot/keyboards"
        "github.com/pweper/bot/internal/bot/middleware"
        "github.com/pweper/bot/internal/user"
)

// showAdminPanel sends the main admin dashboard.
// Called from /admin command or "adm_main" callback.
func (b *Bot) showAdminPanel(c *middleware.Ctx) {
        stats, err := b.users.GetStats(c)
        if err != nil {
                b.logger.Error("GetStats", "err", err)
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Ошибка получения статистики."))
                return
        }

        roleLevel := user.RoleLevel(c.User.Role)
        if c.Cfg.IsAdmin(c.User.ChatID) && roleLevel < 3 {
                // Admin IDs from env are developers by default.
                roleLevel = 3
        }

        roleLabel := "👤 Стафф"
        switch c.User.Role {
        case user.RoleDeveloper:
                roleLabel = "👨‍💻 Разработчик"
        case user.RoleAdmin:
                roleLabel = "🛡 Администратор"
        case user.RoleModerator:
                roleLabel = "🔧 Модератор"
        }
        if c.Cfg.IsAdmin(c.User.ChatID) && c.User.Role == "" {
                roleLabel = "👨‍💻 Разработчик"
        }

        workGB := b.workDirSizeGB()

        text := fmt.Sprintf(
                "🛠 <b>Панель %s</b>\n\n"+
                        "👥 Всего пользователей: <b>%d</b>\n"+
                        "💎 Premium: <b>%d</b>\n"+
                        "🆓 Бесплатных: <b>%d</b>\n"+
                        "🚫 Заблокировано: <b>%d</b>\n"+
                        "📅 Активно сегодня: <b>%d</b>\n"+
                        "💾 Work-папка: <b>%.2f ГБ</b> (лимит %.1f)",
                roleLabel, stats.Total, stats.Paid, stats.Free, stats.Banned, stats.Today,
                workGB, b.cfg.MaxWorkGB)

        msg := tgbotapi.NewMessage(c.Message.Chat.ID, text)
        msg.ParseMode = "HTML"
        msg.ReplyMarkup = keyboards.AdminMain(roleLevel)
        _, _ = b.api.Send(msg)
}

// cbAdminMain — "⬅️ Назад" → admin dashboard.
func (b *Bot) cbAdminMain(c *middleware.Ctx) {
        b.showAdminPanel(c)
}

// cbAdminStats — refresh the dashboard (same as adm_main).
func (b *Bot) cbAdminStats(c *middleware.Ctx) {
        b.showAdminPanel(c)
}

// cbAdminTop — top-10 active users.
func (b *Bot) cbAdminTop(c *middleware.Ctx) {
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

        // Edit the existing message (this is a callback).
        if c.Update.CallbackQuery != nil {
                edit := tgbotapi.NewEditMessageTextAndMarkup(
                        c.Message.Chat.ID, c.Message.MessageID, sb.String(),
                        keyboards.Back())
                edit.ParseMode = "HTML"
                _, _ = b.api.Send(edit)
        }
}

// ── Active subscriptions list (paginated) ─────────────────────────────────

const subsPerPage = 10

// cbAdminSubs — show page 0 of active subscriptions.
func (b *Bot) cbAdminSubs(c *middleware.Ctx, page int) {
        b.showSubsPage(c, page)
}

// cbAdminSubsPage — show a specific page (callback_data = "adm_subs_page_N").
func (b *Bot) cbAdminSubsPage(c *middleware.Ctx, data string) {
        // data = "adm_subs_page_42"
        parts := strings.Split(data, "_")
        if len(parts) < 4 {
                return
        }
        page, err := strconv.Atoi(parts[3])
        if err != nil {
                return
        }
        b.showSubsPage(c, page)
}

// showSubsPage renders one page of the active-subs list.
func (b *Bot) showSubsPage(c *middleware.Ctx, page int) {
        rows, total, err := b.users.ListActiveSubscriptions(c, subsPerPage, page*subsPerPage)
        if err != nil {
                b.logger.Error("ListActiveSubscriptions", "err", err)
                return
        }
        totalPages := (total + subsPerPage - 1) / subsPerPage
        if totalPages < 1 {
                totalPages = 1
        }
        if page >= totalPages {
                page = totalPages - 1
        }

        var sb strings.Builder
        sb.WriteString(fmt.Sprintf("👥 <b>Активные подписки</b> (%d всего, стр. %d/%d)\n\n", total, page+1, totalPages))
        if len(rows) == 0 {
                sb.WriteString("Нет активных подписок.")
        } else {
                for i, r := range rows {
                        num := page*subsPerPage + i + 1
                        var until string
                        if r.IsForever {
                                until = "♾️ бессрочно"
                        } else {
                                until = fmt.Sprintf("до %s (%d дн.)", r.Expiry, r.DaysLeft)
                        }
                        name := r.Username
                        if name == "" {
                                name = "—"
                        }
                        sb.WriteString(fmt.Sprintf("%d. <code>%d</code> %s\n   %s\n", num, r.ChatID, name, until))
                }
        }

        edit := tgbotapi.NewEditMessageTextAndMarkup(
                c.Message.Chat.ID, c.Message.MessageID, sb.String(),
                keyboards.SubsPagination(page, totalPages))
        edit.ParseMode = "HTML"
        _, _ = b.api.Send(edit)
}

// ── User search ───────────────────────────────────────────────────────────

// cbAdminFindPrompt — opens the search prompt (just tells user to type /find).
func (b *Bot) cbAdminFindPrompt(c *middleware.Ctx) {
        text := "🔍 <b>Поиск пользователя</b>\n\n" +
                "Введите команду:\n" +
                "<code>/find ID</code> — поиск по Telegram ID\n" +
                "<code>/find @username</code> — поиск по юзернейму\n" +
                "<code>/find username</code> — без @ тоже работает\n\n" +
                "Пример: <code>/find 123456789</code>"
        edit := tgbotapi.NewEditMessageTextAndMarkup(
                c.Message.Chat.ID, c.Message.MessageID, text, keyboards.Back())
        edit.ParseMode = "HTML"
        _, _ = b.api.Send(edit)
}

// handleAdminFind — /find ID|@username
// Replaces the stub; implements real search via user.Store.Search.
func (b *Bot) handleAdminFind(c *middleware.Ctx) {
        if !c.User.IsAdmin && !c.Cfg.IsAdmin(c.User.ChatID) {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Нет прав."))
                return
        }
        parts := strings.Fields(c.Message.Text)
        if len(parts) < 2 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "Использование: /find ID или /find @username"))
                return
        }
        query := strings.Join(parts[1:], " ")
        results, err := b.users.Search(c, query)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Ошибка: "+err.Error()))
                return
        }
        if len(results) == 0 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "🔍 Ничего не найдено по запросу: "+query))
                return
        }

        for _, u := range results {
                text := b.formatUserCard(&u)
                msg := tgbotapi.NewMessage(c.Message.Chat.ID, text)
                msg.ParseMode = "HTML"
                msg.ReplyMarkup = keyboards.UserCard(u.ChatID)
                _, _ = b.api.Send(msg)
        }
}

// cbAdminFindResult — placeholder for "adm_find_<id>" callback (Phase 3+).
func (b *Bot) cbAdminFindResult(c *middleware.Ctx, data string) {
        // data = "adm_find_<id>"
        parts := strings.Split(data, "_")
        if len(parts) < 3 {
                return
        }
        uid, err := strconv.ParseInt(parts[2], 10, 64)
        if err != nil {
                return
        }
        u, err := b.users.Get(c, uid)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Пользователь не найден."))
                return
        }
        text := b.formatUserCard(u)
        edit := tgbotapi.NewEditMessageTextAndMarkup(
                c.Message.Chat.ID, c.Message.MessageID, text, keyboards.UserCard(u.ChatID))
        edit.ParseMode = "HTML"
        _, _ = b.api.Send(edit)
}

// formatUserCard builds the HTML card for a single user.
// Used by /find and adm_find_<id> callback.
func (b *Bot) formatUserCard(u *user.User) string {
        var sb strings.Builder
        sb.WriteString(fmt.Sprintf("👤 <b>Карточка пользователя</b>\n\n"))
        sb.WriteString(fmt.Sprintf("🆔 ID: <code>%d</code>\n", u.ChatID))
        if u.Username != "" {
                sb.WriteString(fmt.Sprintf("👤 Username: @%s\n", u.Username))
        }
        sb.WriteString("\n<b>Статус:</b>\n")
        if u.IsBanned {
                sb.WriteString(fmt.Sprintf("🚫 Забанен. Причина: %s\n", u.BanReason))
        } else if u.IsSubscribed && !u.IsExpired() {
                sb.WriteString(fmt.Sprintf("💎 Premium: %s\n", user.FormatExpiry(u.Expiry)))
                if u.DaysLeft() >= 0 {
                        sb.WriteString(fmt.Sprintf("   Осталось: %d дн.\n", u.DaysLeft()))
                }
        } else {
                sb.WriteString("🆓 Бесплатный пользователь\n")
                if u.IsSubscribed && u.IsExpired() {
                        sb.WriteString("   ⚠️ Подписка истекла\n")
                }
        }

        sb.WriteString("\n<b>Роль:</b> ")
        switch u.Role {
        case user.RoleDeveloper:
                sb.WriteString("👨‍💻 Разработчик\n")
        case user.RoleAdmin:
                sb.WriteString("🛡 Администратор\n")
        case user.RoleModerator:
                sb.WriteString("🔧 Модератор\n")
        default:
                sb.WriteString("—\n")
        }

        sb.WriteString("\n<b>Рефералы:</b>\n")
        if u.ReferredBy.Valid {
                sb.WriteString(fmt.Sprintf("   Приглашён: <code>%d</code>\n", u.ReferredBy.Int64))
        } else {
                sb.WriteString("   Приглашён: —\n")
        }
        sb.WriteString(fmt.Sprintf("   Баланс: <b>%d ⭐</b>\n", u.RefBalance))

        sb.WriteString("\n<b>Настройки:</b>\n")
        sb.WriteString(fmt.Sprintf("   AI модель: %s\n", u.AIModel))
        sb.WriteString(fmt.Sprintf("   BTX блок: %s | качество: %s | скорость: %s\n",
                u.BTXBlock, u.BTXQuality, u.BTXSpeed))

        if u.TrialUsed {
                sb.WriteString("   Trial: использован\n")
        } else {
                sb.WriteString("   Trial: доступен\n")
        }
        if u.ActivePromo.Valid {
                sb.WriteString(fmt.Sprintf("   Активный промокод: %s\n", u.ActivePromo.String))
        }

        sb.WriteString(fmt.Sprintf("\n📊 Сообщений: %d\n", u.MsgCount))
        if u.CreatedAt != "" {
                sb.WriteString(fmt.Sprintf("📅 Регистрация: %s\n", u.CreatedAt))
        }
        return sb.String()
}

// handleAdminSubs — /subs (text command alias for the callback button).
func (b *Bot) handleAdminSubs(c *middleware.Ctx) {
        if !c.User.IsAdmin && !c.Cfg.IsAdmin(c.User.ChatID) {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Нет прав."))
                return
        }
        // Reuse the callback path by sending the first page as a fresh message.
        rows, total, err := b.users.ListActiveSubscriptions(c, subsPerPage, 0)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Ошибка: "+err.Error()))
                return
        }
        totalPages := (total + subsPerPage - 1) / subsPerPage
        if totalPages < 1 {
                totalPages = 1
        }
        var sb strings.Builder
        sb.WriteString(fmt.Sprintf("👥 <b>Активные подписки</b> (%d всего, стр. 1/%d)\n\n", total, totalPages))
        if len(rows) == 0 {
                sb.WriteString("Нет активных подписок.")
        } else {
                for i, r := range rows {
                        var until string
                        if r.IsForever {
                                until = "♾️ бессрочно"
                        } else {
                                until = fmt.Sprintf("до %s (%d дн.)", r.Expiry, r.DaysLeft)
                        }
                        name := r.Username
                        if name == "" {
                                name = "—"
                        }
                        sb.WriteString(fmt.Sprintf("%d. <code>%d</code> %s\n   %s\n", i+1, r.ChatID, name, until))
                }
        }
        msg := tgbotapi.NewMessage(c.Message.Chat.ID, sb.String())
        msg.ParseMode = "HTML"
        msg.ReplyMarkup = keyboards.SubsPagination(0, totalPages)
        _, _ = b.api.Send(msg)
}

// workDirSizeGB walks the work directory and returns total size in GB.
// Cached for 60 seconds to avoid hammering the filesystem on every /admin.
func (b *Bot) workDirSizeGB() float64 {
        // TODO: implement with filepath.Walk + cached result.
        return 0.0
}

// ── /ban ──────────────────────────────────────────────────────────────────

// handleBan — /ban <id> [reason]
// Replaces stub. Admin-only.
func (b *Bot) handleBan(c *middleware.Ctx) {
        if !c.User.IsAdmin && !c.Cfg.IsAdmin(c.User.ChatID) {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Нет прав."))
                return
        }
        parts := strings.Fields(c.Message.Text)
        if len(parts) < 2 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "Использование: /ban <id> [причина]"))
                return
        }
        uid, err := strconv.ParseInt(parts[1], 10, 64)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Неверный ID."))
                return
        }
        reason := "Нарушение правил"
        if len(parts) > 2 {
                reason = strings.Join(parts[2:], " ")
        }
        if err := b.users.Ban(c, uid, reason); err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Ошибка: "+err.Error()))
                return
        }
        msg := tgbotapi.NewMessage(c.Message.Chat.ID,
                fmt.Sprintf("🚫 Пользователь <code>%d</code> заблокирован.\nПричина: %s", uid, reason))
        msg.ParseMode = "HTML"
        _, _ = b.api.Send(msg)
}

// handleUnban — /unban <id>
func (b *Bot) handleUnban(c *middleware.Ctx) {
        if !c.User.IsAdmin && !c.Cfg.IsAdmin(c.User.ChatID) {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Нет прав."))
                return
        }
        parts := strings.Fields(c.Message.Text)
        if len(parts) < 2 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "Использование: /unban <id>"))
                return
        }
        uid, err := strconv.ParseInt(parts[1], 10, 64)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Неверный ID."))
                return
        }
        if err := b.users.Unban(c, uid); err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Ошибка: "+err.Error()))
                return
        }
        msg := tgbotapi.NewMessage(c.Message.Chat.ID,
                fmt.Sprintf("✅ Пользователь <code>%d</code> разбанен.", uid))
        msg.ParseMode = "HTML"
        _, _ = b.api.Send(msg)
}

// handleGiveSub — /givesub <id> <days>   (-1 = forever)
func (b *Bot) handleGiveSub(c *middleware.Ctx) {
        if !c.User.IsAdmin && !c.Cfg.IsAdmin(c.User.ChatID) {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Нет прав."))
                return
        }
        parts := strings.Fields(c.Message.Text)
        if len(parts) < 3 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        "Использование: /givesub <id> <days>\n-1 = навсегда"))
                return
        }
        uid, err := strconv.ParseInt(parts[1], 10, 64)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Неверный ID."))
                return
        }
        days, err := strconv.Atoi(parts[2])
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Неверное число дней."))
                return
        }
        expiry, err := b.users.GrantSubscription(c, uid, days)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Ошибка: "+err.Error()))
                return
        }
        msg := tgbotapi.NewMessage(c.Message.Chat.ID,
                fmt.Sprintf("✅ Подписка выдана <code>%d</code> до <b>%s</b>", uid, expiry))
        msg.ParseMode = "HTML"
        _, _ = b.api.Send(msg)
}

// handleAddChannel — /addchannel @username
func (b *Bot) handleAddChannel(c *middleware.Ctx) {
        if !c.User.IsAdmin && !c.Cfg.IsAdmin(c.User.ChatID) {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Нет прав."))
                return
        }
        parts := strings.Fields(c.Message.Text)
        if len(parts) < 2 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "Использование: /addchannel @username"))
                return
        }
        ch := parts[1]
        if !strings.HasPrefix(ch, "@") {
                ch = "@" + ch
        }
        if err := b.channels.Add(c, ch); err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Ошибка: "+err.Error()))
                return
        }
        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                fmt.Sprintf("✅ Канал %s добавлен в обязательные.", ch)))
}

// handleDelChannel — /delchannel @username
func (b *Bot) handleDelChannel(c *middleware.Ctx) {
        if !c.User.IsAdmin && !c.Cfg.IsAdmin(c.User.ChatID) {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Нет прав."))
                return
        }
        parts := strings.Fields(c.Message.Text)
        if len(parts) < 2 {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "Использование: /delchannel @username"))
                return
        }
        ch := parts[1]
        if !strings.HasPrefix(ch, "@") {
                ch = "@" + ch
        }
        ok, err := b.channels.Remove(c, ch)
        if err != nil {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID, "❌ Ошибка: "+err.Error()))
                return
        }
        if ok {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        fmt.Sprintf("✅ Канал %s удалён.", ch)))
        } else {
                _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                        fmt.Sprintf("❌ Канал %s не найден.", ch)))
        }
}
