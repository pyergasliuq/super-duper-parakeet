// Package keyboards builds all inline keyboards used by the bot.
//
// Centralised in one place so handlers don't repeat button definitions and
// we can change callback_data format in one spot.
package keyboards

import (
	"fmt"
	"strconv"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/pweper/bot/internal/config"
)

// AdminMain builds the main admin panel keyboard. Level-gated buttons appear
// only if the user has the required role level (1=mod, 2=admin, 3=developer).
func AdminMain(roleLevel int) tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{}

	// Everyone with admin access sees these.
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📊 Статистика", "adm_stats"),
		tgbotapi.NewInlineKeyboardButtonData("🏆 Топ активности", "adm_top"),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("👥 Все подписки", "adm_subs"),
		tgbotapi.NewInlineKeyboardButtonData("🔍 Поиск", "adm_find"),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📈 Графики", "adm_charts"),
		tgbotapi.NewInlineKeyboardButtonData("🎫 Тех. поддержка", "adm_tickets"),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🗑 Очистить work/", "adm_cleanup"),
	))

	// Level 2+ (admin, developer).
	if roleLevel >= 2 {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🚫 Баны", "adm_bans_list"),
			tgbotapi.NewInlineKeyboardButtonData("🎁 Выдать подписку", "adm_givesub_panel"),
		))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📢 Рассылка", "adm_broadcast"),
			tgbotapi.NewInlineKeyboardButtonData("📋 Опросы", "adm_polls_menu"),
		))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🃏 Пранк-опросы", "adm_prank_menu"),
			tgbotapi.NewInlineKeyboardButtonData("🎟 Промокоды", "adm_promos"),
		))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("👥 Рефералы", "adm_ref_stats"),
			tgbotapi.NewInlineKeyboardButtonData("🛒 История покупок", "adm_purchases"),
		))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⭐ Отзывы", "adm_reviews"),
		))
	}

	// Level 3 (developer only).
	if roleLevel >= 3 {
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📣 Каналы подписки", "adm_channels"),
			tgbotapi.NewInlineKeyboardButtonData("👑 Роли", "adm_roles"),
		))
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📋 Лог ролей", "adm_role_log"),
			tgbotapi.NewInlineKeyboardButtonData("👮 Активность стаффа", "adm_staff_stats"),
		))
	}

	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📋 Команды", "adm_commands_list"),
	))
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// Back returns a single "⬅️ Назад" button pointing to adm_main.
func Back() tgbotapi.InlineKeyboardMarkup {
	return tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "adm_main"),
	))
}

// SubsPagination builds the Prev/Next buttons for the active-subs list.
// page is 0-indexed; totalPages is total number of pages.
func SubsPagination(page, totalPages int) tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{}
	var row []tgbotapi.InlineKeyboardButton
	if page > 0 {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(
			"⬅️ Назад", fmt.Sprintf("adm_subs_page_%d", page-1)))
	}
	row = append(row, tgbotapi.NewInlineKeyboardButtonData(
		fmt.Sprintf("%d / %d", page+1, totalPages), "adm_subs_nop"))
	if page+1 < totalPages {
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(
			"Вперёд ➡️", fmt.Sprintf("adm_subs_page_%d", page+1)))
	}
	if len(row) > 0 {
		rows = append(rows, row)
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ В админку", "adm_main"),
	))
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// UserCard builds the action buttons for a found user (in /find result).
func UserCard(chatID int64) tgbotapi.InlineKeyboardMarkup {
	prefix := "adm_user_" + strconv.FormatInt(chatID, 10) + "_"
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎁 Выдать подписку", prefix+"givesub"),
			tgbotapi.NewInlineKeyboardButtonData("🚫 Забанить", prefix+"ban"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✅ Разбанить", prefix+"unban"),
			tgbotapi.NewInlineKeyboardButtonData("📊 История", prefix+"history"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ В админку", "adm_main"),
		),
	)
}

// SubscriptionPlans builds the plan selection keyboard.
func SubscriptionPlans(plans []config.Plan) tgbotapi.InlineKeyboardMarkup {
	rows := [][]tgbotapi.InlineKeyboardButton{}
	for _, p := range plans {
		label := fmt.Sprintf("%s %s — %d ⭐", p.Emoji, p.Label, p.Stars)
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, fmt.Sprintf("buy_%d", p.Stars)),
		))
	}
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonURL("💬 Купить у владельца", "https://t.me/keedboy016"),
	))
	return tgbotapi.NewInlineKeyboardMarkup(rows...)
}

// CheckChannels builds the "subscribe to channels" keyboard.
func CheckChannels(channels []string) tgbotapi.InlineKeyboardMarkup {
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

// BTXSettings builds the BTX settings menu (Phase 6).
func BTXSettings(currentBlock, currentQuality, currentSpeed string) tgbotapi.InlineKeyboardMarkup {
	mark := func(s, cur string) string {
		if s == cur {
			return "✅ " + s
		}
		return s
	}
	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🔷 Блок: "+mark(currentBlock, currentBlock), "stg_btx_block_menu"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🎯 Качество: "+currentQuality, "stg_btx_quality_menu"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⚡ Скорость: "+currentSpeed, "stg_btx_speed_menu"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "stg_main"),
		),
	)
}
