// Package bot — handlers_help.go
//
// Comprehensive help system with inline tips for "дуроки" (users who don't
// read docs). Each command has:
//   - Short description (shown in /help)
//   - Usage examples (shown when called without args or with /help <cmd>)
//   - Common mistakes + how to fix them
//   - Pro tips for advanced users
package bot

import (
        "strings"

        tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

        "github.com/pweper/bot/internal/bot/middleware"
)

// CommandHelp describes one command for the help system.
type CommandHelp struct {
        Name        string // "/color"
        Short       string // one-line description
        Usage       string // example with args
        Description string // detailed explanation
        Examples    []string
        CommonMistakes []string // things users get wrong
        ProTip      string // optional advanced tip
}

// AllCommands is the full help database.
var AllCommands = []CommandHelp{
        // ── Основные ────────────────────────────────────────────────────────────
        {
                Name: "/start", Short: "Запустить бота",
                Usage: "/start", Description: "Главное меню. Если вы перешли по реферальной ссылке, она автоматически зарегистрируется.",
                Examples: []string{"/start", "/start ref_<token>"},
        },
        {
                Name: "/help", Short: "Эта справка",
                Usage: "/help [команда]", Description: "Показывает список всех команд или детальную справку по одной команде.",
                Examples: []string{"/help", "/help /color", "/help /btx"},
        },
        {
                Name: "/mysub", Short: "Статус подписки",
                Usage: "/mysub", Description: "Показывает, активна ли Premium-подписка и до какой даты.",
        },
        {
                Name: "/top", Short: "Топ активности",
                Usage: "/top", Description: "Топ-10 пользователей по количеству сообщений.",
        },
        {
                Name: "/settings", Short: "Настройки BTX",
                Usage: "/settings", Description: "Меню настроек BTX (блок, качество, скорость).",
                ProTip: "Для фото с мелкими деталями выбирайте 4×4 + max_quality — лучший результат.",
        },
        {
                Name: "/ref", Short: "Реферальная программа",
                Usage: "/ref", Description: "Ваша персональная ссылка для приглашения друзей. За каждого оплатившего вы получаете 15% от его покупки в звёздах.",
                ProTip: "При 25 рефералах вы получаете 1 месяц Pro бесплатно.",
        },
        {
                Name: "/refbal", Short: "Баланс рефералов",
                Usage: "/refbal", Description: "Сколько звёзд на балансе. Для вывода напишите @keedboy016.",
        },
        {
                Name: "/promo", Short: "Промокод",
                Usage: "/promo КОД", Description: "Активировать промокод. После активации выберите тариф со скидкой.",
                Examples: []string{"/promo SUMMER50"},
                CommonMistakes: []string{"Промокод не нужен в подписи к файлу — это отдельная команда.", "Промокоды чувствительны к регистру? Нет — мы приводим к верхнему автоматически."},
        },
        {
                Name: "/review", Short: "Оставить отзыв",
                Usage: "/review 5 Отличный бот!", Description: "Оценка от 1 до 5 и необязательный текст.",
        },
        {
                Name: "/support", Short: "Техподдержка",
                Usage: "/support", Description: "Создать обращение в поддержку. Premium-пользователи обслуживаются приоритетно.",
        },

        // ── Работа с цветом ────────────────────────────────────────────────────
        {
                Name: "/color", Short: "Покраска изображения",
                Usage: "/color #RRGGBB [alpha]", Description: "Применяет цвет к изображению, сохраняя яркость. alpha от 0.0 до 1.0 — насколько сильно применить цвет.",
                Examples: []string{"/color #FF0000", "/color #FF0000 0.5", "/color FF0000 0.8"},
                CommonMistakes: []string{
                        "Не отправляйте команду без файла — она работает как подпись к файлу.",
                        "# обязателен перед цветом (или нет — добавим автоматически).",
                        "Alpha > 1.0 или < 0 не работает. По умолчанию 1.0 (полная замена).",
                },
                ProTip: "Для лёгкого оттенка используйте alpha 0.3-0.5. Для полной перекраски — 1.0.",
        },
        {
                Name: "/recolor", Short: "Замена цвета",
                Usage: "/recolor #target #replacement [tolerance]", Description: "Заменяет все пиксели близкие к target на replacement. tolerance 0-255 — насколько широкий диапазон цветов заменить.",
                Examples: []string{"/recolor #ff0000 #00ff00", "/recolor #ffffff #000000 30", "/recolor #ffbbbb #661717 30"},
                CommonMistakes: []string{
                        "tolerance по умолчанию 10. Для замены похожих оттенков увеличьте до 30-50.",
                        "Вместо replacement можно указать 'none' — тогда пиксели станут прозрачными.",
                },
                ProTip: "Чтобы убрать белый фон: /recolor #ffffff none 10",
        },
        {
                Name: "/filters", Short: "Фильтры изображения",
                Usage: "/filters <имя> [сила]", Description: "Применяет фильтр. Сила 0-100 (по умолчанию 50).",
                Examples: []string{"/filters red", "/filters grayscale", "/filters sepia", "/filters light 50", "/filters contrast 30"},
                CommonMistakes: []string{
                        "Доступные фильтры: red, green, blue, grayscale, negate, sepia, solarize, light, saturation, contrast, clarity.",
                        "Названия фильтров регистронезависимы: RED = red.",
                },
                ProTip: "Для vintage-эффекта: /filters sepia, затем /filters light 20",
        },
        {
                Name: "/quality", Short: "Улучшение качества",
                Usage: "/quality <уровень>", Description: "Увеличивает изображение в 1.5× + повышает резкость. Уровень 1-100.",
                Examples: []string{"/quality 16", "/quality 50", "/quality 100"},
                CommonMistakes: []string{"Уровень > 100 даёт тот же результат, что и 100."},
        },
        {
                Name: "/compress", Short: "Сжатие/resize",
                Usage: "/compress <WxH>", Description: "Уменьшает изображение до указанного размера.",
                Examples: []string{"/compress 512x512", "/compress 1024x768"},
                CommonMistakes: []string{"Формат строго WxH (например 512x512), без пробелов."},
        },
        {
                Name: "/overlay", Short: "Наложение изображений",
                Usage: "/overlay <режим> <alpha>", Description: "Накладывает второе изображение на первое. Сначала отправьте первое с подписью, затем второе.",
                Examples: []string{"/overlay multiply 50", "/overlay screen 80", "/overlay add 100"},
                CommonMistakes: []string{
                        "Режимы: multiply, screen, overlay, add, darker.",
                        "alpha 0-100 — насколько сильно второе изображение перекрывает первое.",
                },
        },
        {
                Name: "/aim", Short: "Прицел из samp в BR",
                Usage: "/aim", Description: "Создаёт прицел в виде креста: 4 копии изображения развёрнутые на 90/180/270 градусов.",
                Examples: []string{"Отправить PNG/JPG с подписью /aim"},
        },
        {
                Name: "/checkcolor", Short: "Палитра цвета",
                Usage: "/checkcolor #RRGGBB", Description: "Показывает квадратик 400×500 заданного цвета с HEX-кодом.",
                Examples: []string{"/checkcolor #FF0000", "/checkcolor #abcdef"},
        },
        {
                Name: "/randcolor", Short: "Случайный цвет",
                Usage: "/randcolor", Description: "Генерирует случайный приятный цвет (HSV с фиксированной насыщенностью).",
        },

        // ── Покраска готовых ассетов ───────────────────────────────────────────
        {
                Name: "/hud1, /hud2, /hud3, /hud4", Short: "Покраска HUD",
                Usage: "/hud1 #RRGGBB [alpha]", Description: "Покрасить готовый HUD из assets/zip/hudN.zip.",
                Examples: []string{"/hud1 #FF0000 0.5"},
        },
        {
                Name: "/hp1, /hp2, /hp3", Short: "Элементы худа",
                Usage: "/hp1 #RRGGBB [alpha]", Description: "Покрасить элементы худа (health bar, armor, и т.д.).",
        },
        {
                Name: "/blood", Short: "Кровь",
                Usage: "/blood #RRGGBB [alpha]", Description: "Покрасить текстуры крови.",
        },
        {
                Name: "/tree, /vctree", Short: "Листва",
                Usage: "/tree #RRGGBB [alpha]", Description: "Покрасить текстуры деревьев. /vctree — для Vice City деревьев.",
        },
        {
                Name: "/kp1..9", Short: "Кнопки",
                Usage: "/kp1 #RRGGBB [alpha]", Description: "Покрасить кнопки интерфейса (от 1 до 9).",
        },
        {
                Name: "/carmenu, /speedometer, /road, /casino, /pickup", Short: "Другие элементы",
                Usage: "/cmd #RRGGBB [alpha]", Description: "Покрасить соответствующие ассеты.",
        },

        // ── Создание файлов ────────────────────────────────────────────────────
        {
                Name: "/weapon", Short: "weapon.dat",
                Usage: "/weapon <PT> <RAZB>", Description: "Создаёт weapon.dat с заданным количеством патронов (PT) и разбросом (RAZB) для Desert Eagle.",
                Examples: []string{"/weapon 9 50", "/weapon 100 10"},
                CommonMistakes: []string{
                        "Сначала выберите пресет: /wpr 1, 2, 3 или 4.",
                        "PT — количество патронов (1-9999).",
                        "RAZB — разброс (0 = идеально точно, 100 = очень большой).",
                },
                ProTip: "Пресеты: 1=стандарт, 2=ускор+антик, 3=без перезарядки+динамичный, 4=без перезарядки+статичный.",
        },
        {
                Name: "/wpr", Short: "Выбор пресета weapon",
                Usage: "/wpr <1-4>", Description: "Выбрать пресет для следующего /weapon.",
                Examples: []string{"/wpr 2"},
        },
        {
                Name: "/timecyc", Short: "TimeCycle",
                Usage: "/timecyc #sky_bot #sky_top #cloud #sun", Description: "Создаёт timecyc.json с заданными цветами для неба, облаков и солнца.",
                Examples: []string{"/timecyc #1a2b3c #4d5e6f #778899 #FFEE88"},
                CommonMistakes: []string{
                        "Нужно 4 цвета именно в таком порядке.",
                        "Без # не работает (или работает — мы добавим автоматически).",
                },
        },
        {
                Name: "/colorcyc", Short: "ColorCycle",
                Usage: "/colorcyc <число или #hex>", Description: "Создаёт colorcycle.dat. Если число — все RGB каналы получают это значение (умноженное на 0.01). Если HEX — каждый канал отдельно.",
                Examples: []string{"/colorcyc 1.2", "/colorcyc #FF0000"},
        },
        {
                Name: "/particle", Short: "particle.cfg",
                Usage: "/particle #hex <размер> [trail] [u] [r]", Description: "Создаёт конфиг частиц (кровь/искры) с заданным цветом и размером.",
                Examples: []string{"/particle #FF0000 10", "/particle #FF8800 10 50 0 0"},
                CommonMistakes: []string{"Размер обязателен.", "Trail/U/R — опциональные параметры, по умолчанию 0."},
        },

        // ── Нарезка ────────────────────────────────────────────────────────────
        {
                Name: "/hudcut", Short: "Нарезка HUD",
                Usage: "Отправить изображение с подписью /hudcut", Description: "Автоматически нарезает HUD на отдельные элементы (health, armor, и т.д.) используя анализ прозрачности.",
                ProTip: "Работает лучше с PNG-изображениями, где фон прозрачный.",
        },
        {
                Name: "/map", Short: "Нарезка карты",
                Usage: "Отправить изображение с подписью /map", Description: "Нарезает radar на 14×14 = 196 тайлов.",
        },
        {
                Name: "/remap", Short: "Сборка карты",
                Usage: "Отправить ZIP с 196 тайлами с подписью /remap", Description: "Собирает нарезанную карту обратно в одно изображение.",
                CommonMistakes: []string{"Нужно ровно 196 файлов с именами radar00.png..radar195.png."},
        },
        {
                Name: "/rehud", Short: "Сборка HUD",
                Usage: "Отправить ZIP с подписью /rehud", Description: "Собирает нарезанный HUD обратно.",
        },

        // ── Дублирование файлов ────────────────────────────────────────────────
        {
                Name: "/logo, /tree, /bild", Short: "Дублирование файлов",
                Usage: "Отправить изображение с подписью /logo", Description: "Создаёт ZIP с N копиями файла под разными именами (для совместимости с разными картами).",
        },

        // ── Поиск и выдача готовых файлов ──────────────────────────────────────
        {
                Name: "/search", Short: "Поиск скина",
                Usage: "/search <ID или имя>", Description: "Поиск по skins.txt. Возвращает ID + имя + готовые файлы скина и текстур.",
                Examples: []string{"/search 11", "/search player", "/search player.mod"},
        },
        {
                Name: "/skin", Short: "Выдать скин",
                Usage: "/skin <ID>", Description: "Выдаёт готовый скин + текстуры по ID из assets/skin/.",
                Examples: []string{"/skin 11"},
        },
        {
                Name: "/car", Short: "Выдать машину",
                Usage: "/car <ID>", Description: "Выдаёт готовую .mod модель машины.",
                Examples: []string{"/car 411"},
        },

        // ── BTX ────────────────────────────────────────────────────────────────
        {
                Name: "/btx", Short: "Настройки BTX",
                Usage: "/btx", Description: "Открывает меню настроек BTX. Качество и скорость выбираются кнопками.",
        },
        {
                Name: "BTX авто-конверсия", Short: "PNG/JPG → BTX",
                Usage: "Просто отправьте PNG/JPG/WebP/GIF/BMP без подписи", Description: "Бот автоматически конвертирует изображение в BTX с вашими настройками качества и скорости.",
                ProTip: "Поддерживаемые форматы: PNG, JPG, JPEG, WebP, GIF, BMP, TIFF. Размер сохраняется (нет pow2 padding).",
        },
        {
                Name: "BTX → PNG", Short: "Декодинг BTX",
                Usage: "Просто отправьте .btx файл без подписи", Description: "Бот автоматически декодирует BTX обратно в PNG.",
        },

        // ── Авто-конвертеры ────────────────────────────────────────────────────
        {
                Name: ".txd", Short: "TXD → PNG ZIP",
                Usage: "Просто отправьте .txd файл", Description: "Распаковывает TXD архив, декодирует все текстуры (DXT1/3/5 + 25 форматов) в PNG, упаковывает в ZIP.",
        },
        {
                Name: ".mod", Short: "MOD → DFF",
                Usage: "Просто отправьте .mod файл", Description: "Дешифрует .mod (TEA-8 + key derive) в стандартный .dff.",
        },
        {
                Name: ".ifp", Short: "IFP → ANI",
                Usage: "Просто отправьте .ifp файл", Description: "Конвертирует анимации из BR-формата в стандартный .ani (префикс ANP3).",
        },
        {
                Name: ".cls", Short: "CLS → COL",
                Usage: "Просто отправьте .cls файл", Description: "Конвертирует коллизии из BR-формата в стандартный .col (префикс COL3).",
        },
        {
                Name: ".bpc", Short: "BPC → ZIP",
                Usage: "Просто отправьте .bpc файл", Description: "Дешифрует XOR-шифр .bpc в обычный ZIP.",
        },
        {
                Name: "timecyc.dat", Short: "DAT → JSON",
                Usage: "Просто отправьте timecyc.dat файл", Description: "Конвертирует timecyc.dat в удобный JSON с цветами для каждой погоды.",
        },
        {
                Name: "timecyc.json", Short: "Извлечение цветов",
                Usage: "Просто отправьте timecyc.json файл", Description: "Извлекает 4 ключевых цвета (SkyTop, SkyBottom, Cloud, Sun) в HEX формате.",
        },

        // ── Пакетная обработка ─────────────────────────────────────────────────
        {
                Name: "/batch", Short: "Пакетная обработка",
                Usage: "/batch <команда> [аргументы]", Description: "Запускает пакетный режим: вы отправляете несколько файлов, а бот применяет к каждому команду. Завершите командой /stopbatch.",
                Examples: []string{"/batch /color #FF0000", "/batch /compress 512x512", "/batch /quality 50"},
                CommonMistakes: []string{
                        "Команда должна быть валидной (/color, /filters, /compress, /quality, и т.д.).",
                        "Файлы отправляются ПОСЛЕ /batch, а не вместе с ним.",
                        "Не забудьте /stopbatch — иначе бот будет ждать ещё файлы.",
                },
                ProTip: "Можно /batch /color #FF0000 0.5 — подпись сохранится и применится ко всем файлам.",
        },
        {
                Name: "/stopbatch", Short: "Завершить пакет",
                Usage: "/stopbatch", Description: "Обрабатывает все полученные файлы и завершает пакетный режим.",
        },

        // ── Прочее ─────────────────────────────────────────────────────────────
        {
                Name: "/edit", Short: "Онлайн-редактор",
                Usage: "/edit", Description: "Ссылка на онлайн-фотошоп (Pixlr).",
        },
        {
                Name: "/ptk", Short: "Пипетка",
                Usage: "Отправить изображение с подписью /ptk", Description: "Извлекает палитру из 10 главных цветов изображения.",
        },
}

// handleHelp shows the help menu. With an argument, shows detailed help for
// one command.
func (b *Bot) handleHelpDetailed(c *middleware.Ctx, parts []string) {
        if len(parts) < 2 {
                b.showHelpMenu(c)
                return
        }
        cmdName := strings.ToLower(parts[1])
        if !strings.HasPrefix(cmdName, "/") {
                cmdName = "/" + cmdName
        }
        // Exact match first
        for _, cmd := range AllCommands {
                if strings.EqualFold(strings.ToLower(cmd.Name), cmdName) {
                        b.showCommandHelp(c, cmd)
                        return
                }
        }
        // Partial match (e.g. /help hud1 matches /hud1, /hud2...)
        for _, cmd := range AllCommands {
                if strings.Contains(strings.ToLower(cmd.Name), cmdName) {
                        b.showCommandHelp(c, cmd)
                        return
                }
        }
        _, _ = b.api.Send(tgbotapi.NewMessage(c.Message.Chat.ID,
                "❌ Команда '"+parts[1]+"' не найдена. Используйте /help для списка."))
}

// showHelpMenu shows the full command list, grouped by category.
func (b *Bot) showHelpMenu(c *middleware.Ctx) {
        var sb strings.Builder
        sb.WriteString("📖 <b>Справка по командам</b>\n\n")
        sb.WriteString("Для детальной справки по команде: <code>/help &lt;команда&gt;</code>\n")
        sb.WriteString("Например: <code>/help /color</code>\n\n")

        categories := []struct {
                title string
         cmds []CommandHelp
        }{
                {"📌 Основные", filterByPrefix([]string{"/start", "/help", "/mysub", "/top", "/settings", "/ref", "/refbal", "/promo", "/review", "/support"})},
                {"🎨 Работа с цветом", filterByPrefix([]string{"/color", "/recolor", "/filters", "/quality", "/compress", "/overlay", "/aim", "/checkcolor", "/randcolor"})},
                {"📦 Готовые ассеты (покраска)", filterByPrefix([]string{"/hud1", "/hp1", "/blood", "/tree", "/kp1", "/carmenu"})},
                {"📂 Создание файлов", filterByPrefix([]string{"/weapon", "/wpr", "/timecyc", "/colorcyc", "/particle"})},
                {"✂️ Нарезка", filterByPrefix([]string{"/hudcut", "/map", "/remap", "/rehud"})},
                {"🔄 Дублирование", filterByPrefix([]string{"/logo"})},
                {"🔍 Поиск", filterByPrefix([]string{"/search", "/skin", "/car"})},
                {"🔷 BTX", filterByPrefix([]string{"/btx"})},
                {"📦 Пакетная обработка", filterByPrefix([]string{"/batch", "/stopbatch"})},
                {"🌐 Прочее", filterByPrefix([]string{"/edit", "/ptk"})},
        }

        for _, cat := range categories {
                if len(cat.cmds) == 0 {
                        continue
                }
                sb.WriteString("<b>" + cat.title + ":</b>\n")
                for _, cmd := range cat.cmds {
                        sb.WriteString("• <code>" + cmd.Name + "</code> — " + cmd.Short + "\n")
                }
                sb.WriteString("\n")
        }

        sb.WriteString("📁 <b>Авто-обработка файлов:</b>\n")
        sb.WriteString("Просто отправьте файл без подписи — бот определит тип:\n")
        sb.WriteString("• <code>.png/.jpg/.webp</code> → BTX\n")
        sb.WriteString("• <code>.btx</code> → PNG\n")
        sb.WriteString("• <code>.txd</code> → ZIP с PNG\n")
        sb.WriteString("• <code>.mod</code> → .dff\n")
        sb.WriteString("• <code>.ifp</code> → .ani\n")
        sb.WriteString("• <code>.cls</code> → .col\n")
        sb.WriteString("• <code>.bpc</code> → .zip\n")
        sb.WriteString("• <code>.dat</code> (timecyc) → .json\n")

        msg := tgbotapi.NewMessage(c.Message.Chat.ID, sb.String())
        msg.ParseMode = "HTML"
        _, _ = b.api.Send(msg)
}

// showCommandHelp shows detailed help for one command.
func (b *Bot) showCommandHelp(c *middleware.Ctx, cmd CommandHelp) {
        var sb strings.Builder
        sb.WriteString("📖 <b>" + cmd.Name + "</b>\n")
        sb.WriteString(strings.Repeat("━", 20) + "\n\n")
        sb.WriteString("📋 <b>Описание:</b> " + cmd.Description + "\n\n")
        sb.WriteString("💡 <b>Использование:</b>\n<code>" + cmd.Usage + "</code>\n\n")

        if len(cmd.Examples) > 0 {
                sb.WriteString("📝 <b>Примеры:</b>\n")
                for _, ex := range cmd.Examples {
                        sb.WriteString("• <code>" + ex + "</code>\n")
                }
                sb.WriteString("\n")
        }

        if len(cmd.CommonMistakes) > 0 {
                sb.WriteString("⚠️ <b>Частые ошибки:</b>\n")
                for _, m := range cmd.CommonMistakes {
                        sb.WriteString("• " + m + "\n")
                }
                sb.WriteString("\n")
        }

        if cmd.ProTip != "" {
                sb.WriteString("🚀 <b>Pro Tip:</b> <i>" + cmd.ProTip + "</i>\n")
        }

        msg := tgbotapi.NewMessage(c.Message.Chat.ID, sb.String())
        msg.ParseMode = "HTML"
        _, _ = b.api.Send(msg)
}

// filterByPrefix returns all commands whose Name starts with one of the
// given prefixes (case-insensitive).
func filterByPrefix(prefixes []string) []CommandHelp {
        var out []CommandHelp
        for _, cmd := range AllCommands {
                for _, p := range prefixes {
                        if strings.HasPrefix(strings.ToLower(cmd.Name), strings.ToLower(p)) {
                                out = append(out, cmd)
                                break
                        }
                }
        }
        return out
}

// _help_var is just to ensure middleware import is used (it's used elsewhere).
var _ = middleware.Ctx{}
