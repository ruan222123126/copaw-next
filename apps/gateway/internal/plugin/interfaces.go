package plugin

type ChannelPlugin interface {
	Name() string
	SendText(userID, sessionID, text string) error
}

type ToolPlugin interface {
	Name() string
	Invoke(input map[string]interface{}) (map[string]interface{}, error)
}
