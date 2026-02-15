package channel

import "log"

type ConsoleChannel struct{}

func NewConsoleChannel() *ConsoleChannel {
	return &ConsoleChannel{}
}

func (c *ConsoleChannel) SendText(userID, sessionID, text string) {
	log.Printf("[console] user=%s session=%s text=%s", userID, sessionID, text)
}
