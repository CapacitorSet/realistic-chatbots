package main

import (
	"bufio"
	"encoding/json"
	"github.com/go-redis/redis"
	"gopkg.in/telegram-bot-api.v4"
	"html"
	"os"
	"regexp"
	"strings"
	"time"
	"github.com/satori/go.uuid"
	"path/filepath"
	"io/ioutil"
	"gopkg.in/yaml.v2"
	"strconv"
)

const CHAT_ID = int64(-1001176389792)

type botStruct struct {
	running      bool
	token        string
	uuid         string // To be used in code
	displayName  string // To be used in UI

	*tgbotapi.BotAPI
	Updates    tgbotapi.UpdatesChannel
	Username string
}

func (b *botStruct) cmdPrintln(a string) {
	cmdPrintln(b.displayName + ": " + a)
}

func (b *botStruct) cmdPrintf(format string, a ...interface{}) {
	cmdPrintf(b.displayName + ": " + format, a...)
}

func (b *botStruct) RedisKey(name string) string {
	return b.uuid + "_" + name
}

func (b *botStruct) Up() {
	if b.running {
		return
	}
	if !regexp.MustCompile("\\d+:[A-Za-z]+").MatchString(b.token) {
		b.cmdPrintf("Invalid token %q.", b.token)
		return
	}
	exists, err := redisClient.Exists(b.RedisKey("delays")).Result()
	if err != nil {
		panic(err)
	}
	if exists == 0 {
		b.cmdPrintf("Not yet trained (use \"train %s filename\").\n", b.displayName)
		return
	}
	if b.BotAPI == nil {
		cmdPrintf("%s: Connecting to Telegram...\n", b.displayName)
		botAPI, err := tgbotapi.NewBotAPI(b.token)
		if err != nil {
			b.cmdPrintf("Couldn't connect to Telegram: %s\n", err.Error())
			b.running = false
			updateBotList()
			return
		}

		// botAPI.Debug = true

		b.cmdPrintf("Connected as @%s.\n", botAPI.Self.UserName)

		u := tgbotapi.NewUpdate(0)
		u.Timeout = 60

		updates, err := botAPI.GetUpdatesChan(u)
		if err != nil {
			b.cmdPrintf("Couldn't get updates: %#v\n", err)
			b.running = false; updateBotList()
		}

		b.BotAPI = botAPI
		b.Updates = updates
		b.Username = botAPI.Self.UserName
	} else {
		panic("Not handled yet")
	}
	b.running = true; updateBotList()
	go func() {
		b.Poll()
	}()
}

func (b *botStruct) SendMessageAfter(text string, delay time.Duration) {
	// Average typing speed: 44 wpm
	numWords := strings.Count(text, " ")
	typingDelay := time.Duration(numWords) * time.Minute / 44
	actualDelay := delay - typingDelay
	if actualDelay > 0 {
		time.Sleep(actualDelay)
	}
	b.BotAPI.Send(tgbotapi.NewChatAction(CHAT_ID, tgbotapi.ChatTyping))
	time.Sleep(typingDelay)
	msg := tgbotapi.NewMessage(CHAT_ID, text)
	b.BotAPI.Send(msg)
}

func (b *botStruct) Poll() {
	go func() {
		for {
			if genmode != MODE_REDIS {
				time.Sleep(time.Second)
				continue
			}
			// https://redis.io/commands/zrangebyscore
			resp, err := redisClient.ZRange(b.RedisKey("delays"), 0, 1).Result()
			if err != nil {
				panic(err)
			}
			if len(resp) == 0 {
				continue
			}

			b.cmdPrintf("%#v\n", resp)

			_delay, err := strconv.Atoi(resp[0])
			if err != nil {
				panic(err)
			}
			b.cmdPrintf("Sleeping for %d seconds\n", _delay)
			delay := time.Duration(_delay) * time.Second
			time.Sleep(delay)
			b.SendMessageAfter(b.MarkovGenerate(), delay)
		}
	}()
	for update := range b.Updates {
		if update.Message == nil {
			continue
		}
		if strings.Contains(update.Message.Text, b.Username) {
			b.SendMessageAfter(b.MarkovGenerate(), 0)
			continue
		}
	}
}

func (b *botStruct) Train(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		b.cmdPrintln(err.Error())
		return
	}
	defer file.Close()

	b.cmdPrintln("Training...")
	scanner := bufio.NewScanner(file)
	i := uint64(0)
	var latestTime time.Time
	delays := make(map[int]uint64)
	for scanner.Scan() {
		i++
		type Messaggio struct {
			Text string    `json:"text"`
			Time time.Time `json:"time"`
		}
		var msg Messaggio
		err = json.Unmarshal(scanner.Bytes(), &msg)
		if err != nil {
			b.cmdPrintln(err.Error())
			return
		}
		msg.Text = strings.Replace(msg.Text, "<br>", "\n", -1)
		msg.Text = strings.Replace(msg.Text, "</a>", "", -1)
		msg.Text = strings.Replace(msg.Text, "<code>", "`", -1)
		msg.Text = strings.Replace(msg.Text, "</code>", "`", -1)
		msg.Text = regexp.MustCompile("<a href=\"[^\"]+\">").ReplaceAllString(msg.Text, "")
		msg.Text = html.UnescapeString(msg.Text)
		b.MarkovStore(msg.Text)
		if !latestTime.IsZero() {
			diff := int(msg.Time.Sub(latestTime).Seconds())
			if diff < 0 {
				// fmt.Printf("%s vs %s\n", msg.Time, latestTime)
				// fmt.Println("Warning: negative diff")
				// panic("negative diff")
				continue
			}
			if _, ok := delays[diff]; !ok {
				delays[diff] = 0
			}
			delays[diff]++
		}
		latestTime = msg.Time
		if i%1E4 == 0 {
			b.cmdPrintf("%d (%s)\n", i, latestTime.String())
		}
	}

	if err := scanner.Err(); err != nil {
		b.cmdPrintln(err.Error())
		return
	}

	// https://redis.io/commands/zrangebyscore
	score := float64(0)
	for diff, val := range delays {
		score += float64(val) / float64(i)
		redisClient.ZAdd(b.RedisKey("delays"), redis.Z{
			Score:  score,
			Member: diff,
		})
	}

	b.cmdPrintf("Trained. Use \"up %s\" to start.\n", b.displayName)
}

type fileConfig struct {
	// Telegram token
	Token string `yaml:"token"`
	Username string `yaml:"username"`
	UUID  string `yaml:"uuid"`
}

func spawnBot(relPath string) {
	if !strings.HasSuffix(relPath, ".yaml") && !strings.HasSuffix(relPath, ".yml") {
		relPath += ".yml"
	}
	absPath, _ := filepath.Abs(relPath)
	configBytes, err := ioutil.ReadFile(absPath)
	if err != nil {
		cmdPrintln(err.Error())
		return
	}
	var c fileConfig
	if err = yaml.Unmarshal(configBytes, &c); err != nil {
		cmdPrintln("yaml: " + err.Error())
		return
	}
	filename := strings.TrimSuffix(filepath.Base(absPath), filepath.Ext(absPath))
	if !regexp.MustCompile("\\d+:[A-Za-z]+").MatchString(c.Token) {
		cmdPrintf("yaml: Invalid token %q.", c.Token)
		return
	}
	if c.Username == "" {
		cmdPrintln("yaml: No username")
		return
	}
	if c.UUID == "" {
		c.UUID = uuid.NewV4().String()
		f, err := os.OpenFile(absPath, os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			cmdPrintf("spawn: failed to append UUID: %s", err.Error())
			return
		}
		_, err = f.WriteString("\nuuid: " + c.UUID)
		if err != nil {
			cmdPrintf("spawn: failed to append UUID: %s", err.Error())
			return
		}
	}
	bot := botStruct{
		displayName:  filename,
		token:        c.Token,
		uuid:         c.UUID,
	}
	bots = append(bots, &bot)
	botsByUsername[c.Username] = &bot
	updateBotList()
	bot.Up()
}