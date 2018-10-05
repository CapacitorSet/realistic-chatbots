package main

import (
	"regexp"
	"strings"
)

const SEPARATOR = " "
const MARKOV_LENGTH = 100

func (b *botStruct) MarkovStore(text string) {
	splitted := strings.Split(text, " ")

	for index, word := range splitted {
		if index < len(splitted)-2 {
			pair := b.RedisKey(word + SEPARATOR + splitted[index + 1])
			redisClient.SAdd(pair, splitted[index+2])
			redisClient.SAdd(b.RedisKey("pairs"), pair)
		}
	}
}

func (b *botStruct) MarkovGenerate() string {
	prefixLen := len(b.RedisKey(""))
	// Start the chain with the seed
	key := redisClient.SRandMember(b.RedisKey("pairs")).Val()

	s := strings.Split(key[prefixLen:], " ")

	for i := 1; i < MARKOV_LENGTH; i++ {
		// Strip prefix
		next := redisClient.SRandMember(key).Val()
		s = append(s, next)

		matched, _ := regexp.MatchString(".*[\\.;!?¿¡]$", next)
		if next == "" || matched {
			break
		}

		key = b.RedisKey(s[len(s) - 2] + SEPARATOR + next)
	}

	text := strings.Join(s, " ")
	return text
}
