package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"
)

// Карта названий дней недели на значения time.Weekday
var dayMap = map[string]time.Weekday{
	"вс": time.Sunday,
	"пн": time.Monday,
	"вт": time.Tuesday,
	"ср": time.Wednesday,
	"чт": time.Thursday,
	"пт": time.Friday,
	"сб": time.Saturday,
}

// ScheduleEntry хранит час и минуту для сигнала
type ScheduleEntry struct {
	Hour, Min int
}

func main() {
	// Разбор флагов командной строки
	scheduleStr := flag.String("schedule", "", `Расписание в формате "пн:14:30,15:00;вт:16:00"`)
	soundPath := flag.String("sound", "", "Путь к MP3-файлу для кастомного звука сигнала (опционально)")
	flag.Parse()

	if *scheduleStr == "" {
		fmt.Println(`Использование: go run main.go -schedule "пн:14:30,15:00;вт:16:00" [-sound /path/to/sound.mp3]`)
		os.Exit(1)
	}

	// Парсинг расписания
	schedule, err := parseSchedule(*scheduleStr)
	if err != nil {
		fmt.Printf("Ошибка парсинга расписания: %v\n", err)
		os.Exit(1)
	}

	// Вывод расписания
	fmt.Println("Расписание сигналов:")
	for dayStr, entries := range schedule {
		times := []string{}
		for _, e := range entries {
			times = append(times, fmt.Sprintf("%02d:%02d", e.Hour, e.Min))
		}
		fmt.Printf("%s: %s\n", dayStr, strings.Join(times, ", "))
	}

	if *soundPath != "" {
		fmt.Printf("Используется кастомный звук: %s\n", *soundPath)
	}

	for {
		// Поиск следующего времени сигнала во всём расписании
		nextBeep := findNextBeep(schedule)
		if nextBeep.IsZero() {
			fmt.Println("Больше будущих сигналов не найдено.")
			os.Exit(0)
		}

		sleepDuration := nextBeep.Sub(time.Now())
		if sleepDuration < 0 {
			// Не должно происходить, но пропускаем, если в прошлом
			continue
		}

		fmt.Printf("Следующий сигнал в: %s (спим %v)\n", nextBeep.Format(time.RFC3339), sleepDuration)

		// Спим до следующего времени сигнала
		time.Sleep(sleepDuration)

		// Сигнал
		beep(*soundPath)
	}
}

// parseSchedule парсит строку расписания в map[день][]ScheduleEntry
func parseSchedule(s string) (map[string][]ScheduleEntry, error) {
	schedule := make(map[string][]ScheduleEntry)
	dayParts := strings.Split(s, ";")
	for _, dayPart := range dayParts {
		dayPart = strings.TrimSpace(dayPart)
		if dayPart == "" {
			continue
		}
		parts := strings.SplitN(dayPart, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("некорректная часть дня: %s", dayPart)
		}
		dayStr := strings.ToLower(strings.TrimSpace(parts[0]))
		timeStrs := strings.Split(parts[1], ",")

		var entries []ScheduleEntry
		for _, t := range timeStrs {
			t = strings.TrimSpace(t)
			hour, min, err := parseTime(t)
			if err != nil {
				return nil, fmt.Errorf("некорректное время '%s' для дня '%s': %v", t, dayStr, err)
			}
			entries = append(entries, ScheduleEntry{Hour: hour, Min: min})
		}

		// Сортировка записей по времени для удобного вывода
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].Hour == entries[j].Hour {
				return entries[i].Min < entries[j].Min
			}
			return entries[i].Hour < entries[j].Hour
		})

		schedule[dayStr] = entries
	}

	// Проверка дней
	for dayStr := range schedule {
		if _, ok := dayMap[dayStr]; !ok {
			return nil, fmt.Errorf("некорректный день: %s", dayStr)
		}
	}

	return schedule, nil
}

// parseTime парсит строку HH:MM в час и минуту
func parseTime(t string) (int, int, error) {
	var hour, min int
	_, err := fmt.Sscanf(t, "%d:%d", &hour, &min)
	if err != nil || hour < 0 || hour > 23 || min < 0 || min > 59 {
		return 0, 0, fmt.Errorf("некорректный формат времени")
	}
	return hour, min, nil
}

// findNextBeep находит ближайшее следующее время сигнала из расписания
func findNextBeep(schedule map[string][]ScheduleEntry) time.Time {
	var next time.Time

	for dayStr, entries := range schedule {
		targetDay := dayMap[dayStr]
		for _, entry := range entries {
			candidate := nextOccurrence(targetDay, entry.Hour, entry.Min)
			if next.IsZero() || candidate.Before(next) {
				next = candidate
			}
		}
	}

	return next
}

// nextOccurrence вычисляет следующее время для заданного дня, часа, минуты
func nextOccurrence(targetDay time.Weekday, hour, min int) time.Time {
	now := time.Now()
	currentYear, currentMonth, currentDay := now.Date()
	currentLocation := now.Location()

	// Начало с сегодняшнего дня в целевое время
	candidate := time.Date(currentYear, currentMonth, currentDay, hour, min, 0, 0, currentLocation)

	// Вычисление дней для добавления до следующего целевого дня
	daysToAdd := int((targetDay - now.Weekday() + 7) % 7)
	if daysToAdd == 0 && now.After(candidate) {
		daysToAdd = 7 // Если время прошло сегодня, добавляем 7 дней
	}

	return candidate.AddDate(0, 0, daysToAdd)
}

// beep воспроизводит сигнал или кастомный MP3 в зависимости от ОС
func beep(sound string) {
	var cmd *exec.Cmd
	var err error

	if sound != "" {
		switch runtime.GOOS {
		case "windows":
			// Экранирование пути для PowerShell
			escapedSound := strings.Replace(sound, "'", "''", -1)
			psScript := fmt.Sprintf(`Add-Type -AssemblyName PresentationCore; $mp = New-Object System.Windows.Media.MediaPlayer; $mp.Open([uri]'%s'); Start-Sleep -Milliseconds 500; $mp.Play(); while (!$mp.NaturalDuration.HasTimeSpan) { Start-Sleep -Milliseconds 200 }; Start-Sleep -Seconds $mp.NaturalDuration.TimeSpan.TotalSeconds; $mp.Close()`, escapedSound)
			cmd = exec.Command("powershell", "-sta", "-Command", psScript)
			err = cmd.Run()
		case "darwin":
			cmd = exec.Command("afplay", sound)
			err = cmd.Run()
		case "linux":
			// Требует установки mpg123: sudo apt-get install mpg123 или аналог
			cmd = exec.Command("mpg123", "--quiet", sound)
			err = cmd.Run()
		default:
			fmt.Printf("Неподдерживаемая ОС для кастомного звука: %s\n", runtime.GOOS)
			return
		}
		if err != nil {
			fmt.Printf("Ошибка воспроизведения звука: %v\n", err)
		}
	} else {
		switch runtime.GOOS {
		case "windows":
			cmd = exec.Command("powershell", "-c", "[console]::beep(750, 500)")
			err = cmd.Run()
		default: // unix-подобные
			cmd = exec.Command("echo", "-ne", "\a")
			cmd.Stdout = os.Stdout
			err = cmd.Run()
		}
		if err != nil {
			fmt.Printf("Ошибка сигнала: %v\n", err)
		}
	}
	fmt.Println("Сигнал!")
}
