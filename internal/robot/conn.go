package robot

// MessageHandler The message-handling interface called by the Feishu/DingTalk long connections (implemented by handler.RobotHandler)
type MessageHandler interface {
	HandleMessage(platform, userID, text string) string
}
