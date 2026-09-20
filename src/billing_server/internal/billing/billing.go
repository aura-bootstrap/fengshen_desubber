package billing

// Cost 1 credit = 1 分钟，不足一分钟按一分钟计。
func Cost(durationSec int64) int64 {
	return (durationSec + 59) / 60
}
