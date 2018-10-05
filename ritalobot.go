package main

import (
	"log"
	"fmt"
	"github.com/gdamore/tcell"
	"github.com/go-redis/redis"
	"github.com/rivo/tview"
	"strings"
	"time"
	"math/rand"
	"bufio"
	"os"
	"strconv"
)

const (
	// Each bot uses delays from Redis.
	MODE_REDIS = iota
	// The controller fetches delays from the RNN file.
	MODE_RNN
)

var genmode = MODE_REDIS

var bots []*botStruct
var botsByUsername = make(map[string]*botStruct)
var redisClient *redis.Client
var app *tview.Application
var botList *tview.List

func updateBotList() {
	botList.Clear()
	for _, bot := range bots {
		if bot.running {
			botList.AddItem(bot.displayName, "  [green]up", 0, nil)
		} else {
			botList.AddItem(bot.displayName, "  [red]down", 0, nil)
		}
	}
}

var cmdField *tview.InputField
var cmdView *tview.TextView

func cmdPrintln(a string) {
	cmdView.Write(append([]byte(a), byte('\n')))
}
func cmdPrintf(format string, a ...interface{}) {
	fmt.Fprintf(cmdView, format, a...)
}

func processCmd() {
	cmd := cmdField.GetText()
	cmdField.SetText("")
	fmt.Fprintln(cmdView, "$ "+cmd)
	switch {
	case cmd == "help":
		cmdPrintln(`Commands available:

 * add [bot]
 * train [bot] [filename]
 * up [bot]
 * mode [redis/rnn]
 * help
 * ping
 * exit/quit`)
	case cmd == "quit" || cmd == "exit":
		app.Stop()
	case cmd == "ping":
		cmdPrintln("app: Pong!")
		_, err := redisClient.Ping().Result()
		if err != nil {
			panic(err)
		}
		cmdPrintln("redis: Pong!")
	case strings.HasPrefix(cmd, "add "):
		spawnBot(cmd[4:])
	case strings.HasPrefix(cmd, "up "):
		botName := cmd[3:]
		var bot *botStruct
		for _, item := range bots {
			if item.displayName == botName {
				bot = item
				break
			}
		}
		if bot == nil {
			cmdPrintln("up: No such bot.")
			break
		}
		bot.Up()
	case strings.HasPrefix(cmd, "mode "):
		mode := cmd[5:]
		switch mode {
		case "redis":
			genmode = MODE_REDIS
			cmdPrintln("Mode set to redis.")
		case "rnn":
			genmode = MODE_RNN
			cmdPrintln("Mode set to rnn.")
		default:
			cmdPrintln("Unknown mode (values allowed: redis, rnn)")
		}
	case strings.HasPrefix(cmd, "train "):
		args := strings.Split(cmd[6:], " ")
		if len(args) != 2 {
			cmdPrintln("Syntax: train BOT FILENAME")
			break
		}
		botName := args[0]
		filename := args[1]
		var bot *botStruct
		for _, item := range bots {
			if item.displayName == botName {
				bot = item
				break
			}
		}
		if bot == nil {
			cmdPrintln("train: No such bot.")
			break
		}
		bot.Train(filename)
	default:
		cmdPrintln("Unknown command.")
	}
}

func randomBot() *botStruct {
	if len(bots) == 0 {
		panic("randomBot() with no bots")
	}
	var botsUp []*botStruct
	for _, bot := range bots {
		if bot.running {
			botsUp = append(botsUp, bot)
		}
	}
	if len(botsUp) == 0 {
		panic("randomBot() with no bots up")
	}
	return botsUp[rand.Intn(len(botsUp))]
}

func usernameOrRandom(username string) *botStruct {
	bot, ok := botsByUsername[username]
	if ok {
		return bot
	}
	return randomBot()
}

func main() {
	rand.Seed(time.Now().UnixNano())
	app = tview.NewApplication()
	botList = tview.NewList().
		ShowSecondaryText(true)
	botList.
		SetBorder(true).
		SetTitle("Bots")
	cmdField = tview.NewInputField().
		SetDoneFunc(func(_ tcell.Key) {
			go processCmd()
		}).
		SetFieldTextColor(tcell.ColorBlack).
		SetFieldBackgroundColor(tcell.ColorWhite)
	cmdView = tview.NewTextView().
		SetText("Write \"help\" to get started.\n").
		SetChangedFunc(func() {
			app.Draw()
		}).
		ScrollToEnd()
	log.SetOutput(cmdView)
	cmdFlex := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(cmdView, 0, 1, false).
		AddItem(cmdField, 1, 0, true)
	globalFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(botList, 20, 1, false).
		AddItem(cmdFlex, 0, 4, true)
	go func() {
		redisClient = redis.NewClient(&redis.Options{
			Addr: ":6379",
		})
		_, err := redisClient.Ping().Result()
		if err != nil {
			panic(err)
		}
		cmdPrintln("redis: Connected.")
	}()
	go func() {
		type delayStruct struct {
			author string
			delay uint64
		}
		var delays []delayStruct
		for {
			if genmode != MODE_RNN {
				time.Sleep(time.Second)
				continue
			}
			if delays == nil {
				file, err := os.Open("delays.txt")
				if err != nil {
					panic(err)
				}
				scanner := bufio.NewScanner(file)
				for scanner.Scan() {
					row := scanner.Text()
					// fmt.Println("Parsing " + row)
					last := strings.LastIndex(row, " ")
					if last == -1 {
						continue
					}
					name := row[:last]
					delay, err := strconv.ParseUint(row[last + 1:], 10, 64)
					if err != nil {
						continue
						// panic(err)
					}
					delays = append(delays, delayStruct{name, delay})
				}

				if err := scanner.Err(); err != nil {
					panic(err)
				}
				file.Close()
			}
			if len(delays) == 0 {
				cmdPrintln("rnn: no more delays!")
				time.Sleep(time.Second)
				continue
			}
			delay := delays[0]
			delays = delays[1:]
			var bot *botStruct
			switch delay.author {
			/* Custom shortcuts here. Todo: load from config */
			default:
				bot = randomBot()
			}
			bot.SendMessageAfter(bot.MarkovGenerate(), time.Duration(delay.delay) * time.Second)
		}
	}()
	err := app.SetRoot(globalFlex, true).SetFocus(globalFlex).Run()
	if err != nil {
		panic(err)
	}
	redisClient.Close()
}
