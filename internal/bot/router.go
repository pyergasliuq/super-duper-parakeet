// Package bot — router.go
//
// Routes updates to the right handler based on:
//   - For text messages: the first token (command). Uses HasPrefix, NOT
//     substring "in" (the original Python had "/hud" matching "/hudcut").
//   - For documents: file extension OR caption command.
//   - For callback queries: callback_data prefix.
package bot

import (
        "strings"

        tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

        "github.com/pweper/bot/internal/bot/middleware"
)

// route is the final step in the middleware chain — dispatches to handlers.
func (b *Bot) route(c *middleware.Ctx) {
        switch {
        case c.Update.CallbackQuery != nil:
                b.routeCallback(c)
        case c.Message != nil:
                b.routeMessage(c)
        }
}

// routeMessage dispatches a single Message.
func (b *Bot) routeMessage(c *middleware.Ctx) {
        m := c.Message
        switch {
        case m.Document != nil:
                b.handleDocumentReal(c)
        case m.Photo != nil:
                _ = b.handleLog(c, "фото")
        case m.Sticker != nil:
                _ = b.handleLog(c, "стикер")
        case m.Animation != nil:
                _ = b.handleLog(c, "гифка")
        case m.Video != nil:
                _ = b.handleLog(c, "видео")
        case m.Voice != nil:
                _ = b.handleLog(c, "голосовое")
        case m.Audio != nil:
                _ = b.handleLog(c, "аудио")
        case m.VideoNote != nil:
                _ = b.handleLog(c, "видео-сообщение")
        case m.Contact != nil:
                _ = b.handleLog(c, "контакт")
        case m.Location != nil:
                _ = b.handleLog(c, "геолокация")
        case m.Poll != nil:
                _ = b.handleLog(c, "опрос", m.Poll.Question)
        case m.Text != "":
                b.routeText(c)
        }
}

// routeText dispatches a text message based on its command prefix.
//
// IMPORTANT: uses HasPrefix on the first whitespace-separated token, NOT
// substring match. This fixes the "/hud" matches "/hudcut" bug from the
// original Python where every `if '/hud' in message.text` was a substring
// check.
func (b *Bot) routeText(c *middleware.Ctx) {
        m := c.Message
        text := m.Text
        if text == "" {
                return
        }
        if !strings.HasPrefix(text, "/") {
                // Plain text message (not a command). Log + ignore.
                _ = b.handleLog(c, "текст")
                return
        }

        // Extract command token (without leading slash, lowercased, without @botname suffix).
        parts := strings.Fields(text)
        if len(parts) == 0 {
                return
        }
        cmdToken := parts[0]
        cmdToken = strings.TrimPrefix(cmdToken, "/")
        if i := strings.IndexByte(cmdToken, '@'); i >= 0 {
                cmdToken = cmdToken[:i]
        }
        cmdToken = strings.ToLower(cmdToken)

        switch cmdToken {
        case "start":
                b.handleStart(c)
        case "help":
                b.handleHelpDetailed(c, parts)
        case "mysub":
                b.handleMySub(c)
        case "top":
                b.handleTop(c)
        case "admin":
                if len(parts) >= 2 {
                        switch parts[1] {
                        case "backup":
                                b.handleAdminBackup(c)
                                return
                        case "metrics", "stats_live":
                                b.handleAdminMetrics(c)
                                return
                        case "errors":
                                b.handleAdminErrors(c)
                                return
                        case "audit":
                                b.handleAdminAudit(c)
                                return
                        }
                }
                b.handleAdmin(c)
        case "find":
                b.handleAdminFind(c)
        case "subs":
                b.handleAdminSubs(c)
        case "addchannel":
                b.handleAddChannel(c)
        case "delchannel":
                b.handleDelChannel(c)
        case "ban":
                b.handleBan(c)
        case "unban":
                b.handleUnban(c)
        case "givesub":
                b.handleGiveSub(c)
        default:
                // Try the catch-all for processing commands like /hud1, /color, etc.
                b.routeTextCommand(c, cmdToken, parts)
        }
}

// routeTextCommand dispatches the processing commands (long tail).
// Separated from routeText so the switch above stays readable.
func (b *Bot) routeTextCommand(c *middleware.Ctx, cmd string, parts []string) {
        switch {
        case cmd == "color", cmd == "recolor", cmd == "filters", cmd == "quality",
                cmd == "compress", cmd == "aim", cmd == "overlay",
                cmd == "logo", cmd == "tree", cmd == "bild", cmd == "map", cmd == "remap",
                cmd == "hudcut", cmd == "rehud", cmd == "genrl", cmd == "bpc", cmd == "nri",
                cmd == "merger", cmd == "index", cmd == "ptk":
                // These are caption-style commands — they expect a file in the same
                // message. If sent as text alone, show usage hint.
                b.replyUsageHint(c, cmd)
        case strings.HasPrefix(cmd, "hud"):
                b.handleHUDColor(c, cmd, parts)
        case strings.HasPrefix(cmd, "hp"):
                b.handleHUDColor(c, cmd, parts)
        case cmd == "blood", cmd == "vctree", cmd == "carmenu",
                cmd == "speedometer", cmd == "road", cmd == "casino", cmd == "pickup":
                b.handleColorCommand(c, cmd, parts)
        case strings.HasPrefix(cmd, "kp"):
                b.handleColorCommand(c, cmd, parts)
        case cmd == "timecyc":
                b.handleTimecyc(c, parts)
        case cmd == "colorcyc":
                b.handleColorcyc(c, parts)
        case cmd == "checkcolor":
                b.handleCheckColor(c, parts)
        case cmd == "randcolor":
                b.handleRandColor(c)
        case cmd == "edit":
                b.handleEdit(c)
        case cmd == "weapon":
                b.handleWeapon(c, parts)
        case cmd == "weapon_all":
                b.handleWeaponAll(c, parts)
        case cmd == "wpr":
                b.handleWPR(c, parts)
        case cmd == "particle":
                b.handleParticle(c, parts)
        case cmd == "btx":
                b.handleBTXSettings(c, parts)
        case cmd == "btxpreview":
                b.handleBTXPreview(c)
        case cmd == "timecyc_preset" || cmd == "timecycpreset":
                b.handleTimecycPreset(c, parts)
        case cmd == "txd_info":
                b.handleTXDInfo(c)
        case cmd == "img2webp":
                b.handleImg2WebP(c)
        case cmd == "dff2mod":
                b.handleDFF2MOD(c)
        case cmd == "pvr2png":
                b.handlePVR2PNG(c)
        case cmd == "search":
                b.handleSearch(c, parts)
        case cmd == "skin":
                b.handleSkin(c, parts)
        case cmd == "car":
                b.handleCar(c, parts)
        case cmd == "promo":
                b.handlePromo(c, parts)
        case cmd == "refbal":
                b.handleRefBal(c)
        case cmd == "ref":
                b.handleRef(c)
        case cmd == "support":
                b.handleSupport(c)
        case cmd == "review":
                b.handleReview(c, parts)
        case cmd == "settings":
                b.handleSettings(c)
        case cmd == "batch":
                b.handleBatch(c, parts)
        case cmd == "stopbatch":
                b.handleStopBatch(c)
        case cmd == "sub":
                b.handleSubAdmin(c, parts)
        case cmd == "kotek":
                b.handleBroadcast(c, parts)
        case cmd == "send":
                b.handleSendAdmin(c, parts)
        default:
                // Unknown command — ignore silently (the Python version logged every
                // single unknown command, filling up the log file).
                b.logger.Debug("unknown command", "cmd", cmd, "user_id", c.User.ChatID)
        }
}

// routeCallback dispatches a callback query.
func (b *Bot) routeCallback(c *middleware.Ctx) {
        cb := c.Update.CallbackQuery
        data := cb.Data
        if data == "" {
                return
        }

        // Admin panel callbacks.
        switch {
        case data == "adm_main":
                b.cbAdminMain(c)
        case data == "adm_stats":
                b.cbAdminStats(c)
        case data == "adm_top":
                b.cbAdminTop(c)
        case data == "adm_subs":
                b.cbAdminSubs(c, 0)
        case strings.HasPrefix(data, "adm_subs_page_"):
                b.cbAdminSubsPage(c, data)
        case data == "adm_find":
                b.cbAdminFindPrompt(c)
        case strings.HasPrefix(data, "adm_find_"):
                b.cbAdminFindResult(c, data)
        case data == "adm_cleanup":
                b.cbAdminCleanup(c)
        case data == "adm_bans_list":
                b.cbAdminBans(c)
        case data == "adm_channels":
                b.cbAdminChannels(c)
        case data == "adm_broadcast":
                b.cbAdminBroadcast(c)
        case data == "adm_givesub_panel":
                b.cbAdminGiveSubPanel(c)
        case data == "adm_roles":
                b.cbAdminRoles(c)
        case data == "adm_role_log":
                b.cbAdminRoleLog(c)
        case data == "adm_staff_stats":
                b.cbAdminStaffStats(c)
        case data == "adm_polls_menu":
                b.cbAdminPollsMenu(c)
        case data == "adm_prank_menu":
                b.cbAdminPrankMenu(c)
        case data == "adm_reviews":
                b.cbAdminReviews(c)
        case data == "adm_tickets":
                b.cbAdminTickets(c)
        case data == "adm_purchases":
                b.cbAdminPurchases(c)
        case data == "adm_ref_stats":
                b.cbAdminRefStats(c)
        case data == "adm_promos":
                b.cbAdminPromos(c)
        case data == "adm_charts":
                b.cbAdminCharts(c)
        case data == "adm_commands_list":
                b.cbAdminCommandsList(c)
        case data == "recheck_channels":
                b.cbRecheckChannels(c)
        case data == "buy_trial":
                b.cbBuyTrial(c)
        case data == "show_plans":
                b.cbShowPlans(c)
        case strings.HasPrefix(data, "buy_"):
                b.cbBuy(c, data)
        case strings.HasPrefix(data, "promo_apply_"):
                b.cbPromoApply(c, data)
        case strings.HasPrefix(data, "support_new"):
                b.cbSupportNew(c)
        case strings.HasPrefix(data, "stg_"):
                b.cbSettings(c, data)
        default:
                b.logger.Debug("unknown callback", "data", data, "user_id", c.User.ChatID)
        }
}

// replyUsageHint sends a short "/cmd — upload a file with this caption" hint
// for caption-style commands invoked without a file.
func (b *Bot) replyUsageHint(c *middleware.Ctx, cmd string) {
        examples := map[string]string{
                "color":    "/color #FF0000 0.4",
                "recolor":  "/recolor #ffbbbb #661717 30",
                "filters":  "/filters red  или  /filters light 100",
                "quality":  "/quality 16",
                "compress": "/compress 512x512",
                "aim":      "пришлите PNG/JPG с подписью /aim",
                "overlay":  "/overlay multiply 50",
                "logo":     "пришлите изображение с подписью /logo",
                "tree":     "пришлите изображение с подписью /tree",
                "bild":     "пришлите изображение с подписью /bild",
                "map":      "пришлите изображение с подписью /map",
                "remap":    "пришлите .zip с подписью /remap",
                "hudcut":   "пришлите изображение с подписью /hudcut",
                "rehud":    "пришлите .zip с подписью /rehud",
                "genrl":    "пришлите .zip/.bpc с подписью /genrl",
                "bpc":      "пришлите .zip с подписью /bpc",
                "nri":      "пришлите .zip с подписью /nri",
                "merger":   "/merger <имя_текстуры> (и приложите .zip/.bpc)",
                "index":    "пришлите .zip с подписью /index",
                "ptk":      "пришлите изображение с подписью /ptk",
        }
        ex := examples[cmd]
        if ex == "" {
                ex = "пришлите файл с подписью /" + cmd
        }
        msg := tgbotapi.NewMessage(c.Message.Chat.ID,
                "❔ <b>Использование:</b> <code>"+ex+"</code>\n\n"+
                        "Эта команда работает как подпись к файлу.")
        msg.ParseMode = "HTML"
        msg.ReplyToMessageID = c.Message.MessageID
        _, _ = b.api.Send(msg)
}
