package spawn

const QuickSettingClose = "close"

func CloseAfterSend(setting string) bool { return setting == QuickSettingClose }
