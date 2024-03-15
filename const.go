package prompt

const (
	timeFormat            = "2006-01-02 15:04:05.000"
	timeFormatRegexString = `(\d{4})-(\d{2})-(\d{2}) (\d{2}):(\d{2}):(\d{2})\.(\d{3})`

	// history format: "{time like timeFormat}: {cmd}"
	historyFormat = "%s: %s"
)
