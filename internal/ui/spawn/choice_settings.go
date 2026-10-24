package spawn

// QuickSettingClose is the value of the quick-close setting that takes the
// bar down after a confirmed send.
const QuickSettingClose = "close"

// CloseAfterSend is whether a confirmed send takes the quick bar down.
func CloseAfterSend(setting string) bool { return setting == QuickSettingClose }
