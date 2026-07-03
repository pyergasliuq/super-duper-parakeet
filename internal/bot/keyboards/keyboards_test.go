// Package keyboards — keyboards_test.go
//
// Tests verify that keyboards build without panicking and that callback_data
// strings match what the router expects.
package keyboards

import (
        "strings"
        "testing"

        "github.com/pweper/bot/internal/config"
)

func TestAdminMain_Level1(t *testing.T) {
        kb := AdminMain(1)
        if len(kb.InlineKeyboard) == 0 {
                t.Fatal("expected non-empty keyboard")
        }
        // Level 1 (moderator) should NOT see "👑 Роли".
        for _, row := range kb.InlineKeyboard {
                for _, btn := range row {
                        if btn.Text == "👑 Роли" {
                                t.Error("moderator should not see Roles button")
                        }
                }
        }
}

func TestAdminMain_Level3(t *testing.T) {
        kb := AdminMain(3)
        // Level 3 (developer) should see Roles.
        found := false
        for _, row := range kb.InlineKeyboard {
                for _, btn := range row {
                        if btn.Text == "👑 Роли" {
                                found = true
                        }
                }
        }
        if !found {
                t.Error("developer should see Roles button")
        }
}

func TestAdminMain_HasStatsAndFind(t *testing.T) {
        kb := AdminMain(1)
        findStats, findFind := false, false
        for _, row := range kb.InlineKeyboard {
                for _, btn := range row {
                        if strings.Contains(btn.Text, "Статистика") {
                                findStats = true
                        }
                        if strings.Contains(btn.Text, "Поиск") {
                                findFind = true
                        }
                }
        }
        if !findStats {
                t.Error("missing Статистика button")
        }
        if !findFind {
                t.Error("missing Поиск button")
        }
}

func TestSubsPagination(t *testing.T) {
        // Page 0 of 3.
        kb := SubsPagination(0, 3)
        hasNext, hasPrev := false, false
        for _, row := range kb.InlineKeyboard {
                for _, btn := range row {
                        if strings.Contains(btn.Text, "Вперёд") {
                                hasNext = true
                        }
                        if strings.Contains(btn.Text, "Назад") {
                                hasPrev = true
                        }
                }
        }
        if !hasNext {
                t.Error("page 0 should have Next button")
        }
        if hasPrev {
                t.Error("page 0 should NOT have Prev button")
        }

        // Page 2 of 3.
        kb = SubsPagination(2, 3)
        hasNext, hasPrev = false, false
        for _, row := range kb.InlineKeyboard {
                for _, btn := range row {
                        if strings.Contains(btn.Text, "Вперёд") {
                                hasNext = true
                        }
                        if strings.Contains(btn.Text, "Назад") {
                                hasPrev = true
                        }
                }
        }
        if hasNext {
                t.Error("last page should NOT have Next button")
        }
        if !hasPrev {
                t.Error("last page should have Prev button")
        }
}

func TestUserCard(t *testing.T) {
        kb := UserCard(123)
        // Should have givesub/ban/unban/history buttons.
        prefix := "adm_user_123_"
        countButtons := 0
        for _, row := range kb.InlineKeyboard {
                for _, btn := range row {
                        if btn.CallbackData != nil && strings.HasPrefix(*btn.CallbackData, prefix) {
                                countButtons++
                        }
                }
        }
        if countButtons < 4 {
                t.Errorf("expected at least 4 action buttons, got %d", countButtons)
        }
}

func TestSubscriptionPlans(t *testing.T) {
        plans := []config.Plan{
                {Stars: 50, Days: 14, Label: "2 недели", Emoji: "⚡"},
                {Stars: 100, Days: 30, Label: "1 месяц", Emoji: "🔥"},
        }
        kb := SubscriptionPlans(plans)
        // Should have 2 plan buttons + 1 URL button = 3 rows.
        if len(kb.InlineKeyboard) != 3 {
                t.Errorf("expected 3 rows, got %d", len(kb.InlineKeyboard))
        }
        // First button should be "⚡ 2 недели — 50 ⭐" with callback "buy_50".
        btn := kb.InlineKeyboard[0][0]
        if btn.Text != "⚡ 2 недели — 50 ⭐" {
                t.Errorf("first button text = %q", btn.Text)
        }
        if btn.CallbackData == nil || *btn.CallbackData != "buy_50" {
                t.Errorf("first button callback = %v", btn.CallbackData)
        }
}

func TestCheckChannels(t *testing.T) {
        channels := []string{"@pweper", "@nonerai"}
        kb := CheckChannels(channels)
        // 2 channel buttons + 1 recheck = 3 rows.
        if len(kb.InlineKeyboard) != 3 {
                t.Errorf("expected 3 rows, got %d", len(kb.InlineKeyboard))
        }
        // First button URL should be https://t.me/pweper.
        btn := kb.InlineKeyboard[0][0]
        if btn.URL == nil || *btn.URL != "https://t.me/pweper" {
                t.Errorf("URL = %v, want https://t.me/pweper", btn.URL)
        }
        // Last button callback should be "recheck_channels".
        last := kb.InlineKeyboard[2][0]
        if last.CallbackData == nil || *last.CallbackData != "recheck_channels" {
                t.Errorf("recheck callback = %v", last.CallbackData)
        }
}
