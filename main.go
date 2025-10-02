package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const dataFile = "vkids.json"

type UserData struct {
	Vkids         []string `json:"vkids"`          // каждая запись — строка вида "04-05 Fri 14:30"
	ReminderHours int      `json:"reminder_hours"` // 0 = выкл, -1 = ожидание ввода
}

var users = make(map[int64]*UserData)

// Загрузка данных из JSON
func loadOrCreateData() {
	if _, err := os.Stat(dataFile); os.IsNotExist(err) {
		saveData()
		return
	}

	data, err := os.ReadFile(dataFile)
	if err != nil {
		log.Printf("Ошибка чтения %s: %v", dataFile, err)
		return
	}

	var allUsers map[string]*UserData
	if err := json.Unmarshal(data, &allUsers); err != nil {
		log.Printf("Ошибка парсинга JSON: %v", err)
		return
	}

	for k, v := range allUsers {
		if id, err := strconv.ParseInt(k, 10, 64); err == nil {
			users[id] = v
		}
	}
}

// Сохранение данных в JSON
func saveData() {
	serializable := make(map[string]*UserData)
	for k, v := range users {
		serializable[strconv.FormatInt(k, 10)] = v
	}

	data, err := json.MarshalIndent(serializable, "", "  ")
	if err != nil {
		log.Printf("Ошибка сериализации: %v", err)
		return
	}

	if err := os.WriteFile(dataFile, data, 0644); err != nil {
		log.Printf("Ошибка записи %s: %v", dataFile, err)
	}
}

// Форматирует текущее время как "MM-dd EEE HH:MM"
func getFormattedNow() string {
	return time.Now().Add(3 * time.Hour).Format("01-02 Mon 15:04")
}

// Парсит строку вида "01-02 Mon 15:04" → time.Time
func parseVkidTime(s string) (time.Time, error) {
	parts := strings.Fields(s)
	if len(parts) < 3 {
		return time.Time{}, fmt.Errorf("неверный формат: %s", s)
	}
	datePart := parts[0] // MM-dd
	timePart := parts[2] // HH:MM

	now := time.Now().Add(3 * time.Hour)
	currentYear := now.Year()

	// Попробуем текущий год
	layout := "2006-01-02 15:04"
	candidateStr := fmt.Sprintf("%d-%s %s", currentYear, datePart, timePart)
	t, err := time.ParseInLocation(layout, candidateStr, time.Local)
	if err != nil {
		return time.Time{}, err
	}

	// Если дата больше чем на 6 месяцев в будущем — значит, это прошлый год
	if t.After(now.AddDate(0, 6, 0)) {
		t = t.AddDate(-1, 0, 0)
	}
	// Если дата больше чем на 6 месяцев в прошлом — значит, это следующий год (редко, но возможно)
	if t.Before(now.AddDate(0, -6, 0)) {
		t = t.AddDate(1, 0, 0)
	}

	return t, nil
}

// Возвращает строку "H:MM" — сколько прошло с последнего вкида
func timeSinceLast(vkids []string) string {
	if len(vkids) == 0 {
		return "Нет записей"
	}
	last := vkids[len(vkids)-1]
	t, err := parseVkidTime(last)
	if err != nil {
		return "Ошибка чтения даты"
	}
	duration := time.Since(t)
	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	return fmt.Sprintf("%d:%02d", hours, minutes)
}

type BOT struct {
	bot *tgbotapi.BotAPI
	mu  *sync.Mutex
}

func (b *BOT) Send(c tgbotapi.Chattable) (tgbotapi.Message, error) {
	b.mu.Lock()

	msg, err := b.bot.Send(c)
	if err != nil {
		return tgbotapi.Message{}, err
	}

	return msg, nil
}

func main() {
	token := "8244558007:AAENGj8YGU0irK5W4O6PnNQXR-88100cNpU"

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Fatal(err)
	}

	Bot := &BOT{
		bot: bot,
		mu:  &sync.Mutex{},
	}

	log.Printf("Бот запущен как @%s", bot.Self.UserName)

	loadOrCreateData()

	// Получение обновлений
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	// Горутина для напоминаний

	for update := range updates {
		// Обработка сообщений
		if update.Message != nil {
			chatID := update.Message.Chat.ID
			text := strings.TrimSpace(update.Message.Text)

			if _, exists := users[chatID]; !exists {
				users[chatID] = &UserData{Vkids: []string{}, ReminderHours: 0}
			}
			userData := users[chatID]

			switch text {
			case "/start":
				kb := tgbotapi.NewReplyKeyboard(
					tgbotapi.NewKeyboardButtonRow(
						tgbotapi.NewKeyboardButton("вкинулся"),
						tgbotapi.NewKeyboardButton("сколько прошло"),
					),
					tgbotapi.NewKeyboardButtonRow(
						tgbotapi.NewKeyboardButton("Меню"),
					),
				)
				msg := tgbotapi.NewMessage(chatID, "Готов отслеживать твои вкиды!")
				msg.ReplyMarkup = kb
				Bot.bot.Send(msg)

			case "вкинулся":
				timeSleep := users[chatID].ReminderHours
				setRemind(Bot, time.Now(), chatID, timeSleep)
				formatted := getFormattedNow() // например: "04-05 Fri 14:30"
				userData.Vkids = append(userData.Vkids, formatted)
				saveData()
				Bot.bot.Send(tgbotapi.NewMessage(chatID, "✅ Записано: "+formatted))

			case "сколько прошло":
				result := timeSinceLast(userData.Vkids)
				Bot.bot.Send(tgbotapi.NewMessage(chatID, "⏱ Прошло: "+result))

			case "/file":
				if _, err := os.Stat(dataFile); err == nil {
					Bot.bot.Send(tgbotapi.NewDocument(chatID, tgbotapi.FilePath(dataFile)))
				} else {
					Bot.bot.Send(tgbotapi.NewMessage(chatID, "Файл данных пока пуст. Сделай хотя бы один 'вкид'."))
				}
			case "Меню":
				inlineKeyboard := tgbotapi.NewInlineKeyboardMarkup(
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("🔁 Периодичность вкида", "set_reminder"),
					),
					tgbotapi.NewInlineKeyboardRow(
						tgbotapi.NewInlineKeyboardButtonData("🗑 Удалить последний вкид", "delete_last"),
					),
				)
				msg := tgbotapi.NewMessage(chatID, "⚙️ Настройки:")
				msg.ReplyMarkup = inlineKeyboard
				Bot.bot.Send(msg)

			default:
				// Ожидание ввода периода
				if userData.ReminderHours == -1 {
					if hours, err := strconv.Atoi(text); err == nil && hours > 0 {
						userData.ReminderHours = hours
						saveData()
						Bot.bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🔔 Напоминания каждые %d ч.", hours)))
					} else {
						Bot.bot.Send(tgbotapi.NewMessage(chatID, "❌ Введите целое число > 0."))
					}
				}
			}
		}

		// Обработка inline-кнопок
		if update.CallbackQuery != nil {
			cb := update.CallbackQuery
			chatID := cb.Message.Chat.ID

			if _, exists := users[chatID]; !exists {
				users[chatID] = &UserData{Vkids: []string{}, ReminderHours: 0}
			}
			userData := users[chatID]

			var resp tgbotapi.CallbackConfig
			switch cb.Data {
			case "set_reminder":
				userData.ReminderHours = -1
				saveData()
				resp = tgbotapi.NewCallback(cb.ID, "")
				bot.Request(resp)
				Bot.bot.Send(tgbotapi.NewMessage(chatID, "Введите периодичность напоминаний (часы):"))

			case "delete_last":
				if len(userData.Vkids) == 0 {
					resp = tgbotapi.NewCallback(cb.ID, "Нет записей")
				} else {
					userData.Vkids = userData.Vkids[:len(userData.Vkids)-1]
					saveData()
					resp = tgbotapi.NewCallback(cb.ID, "Удалено")
				}
				Bot.bot.Request(resp)

			}
		}
	}
}

func setRemind(Bot *BOT, timeNow time.Time, id int64, timeWait int) {
	go func() {

		time.Sleep(time.Duration(timeWait) * time.Hour)

		msg := tgbotapi.NewMessage(id, fmt.Sprintf("Прошло %d часов с последнего вкида!", timeWait))
		Bot.bot.Send(msg)

	}()
}
