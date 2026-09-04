package repository

import "time"

// timeFromUnix 将 unix 秒转为 time.Time
func timeFromUnix(unixSec int64) time.Time {
	return time.Unix(unixSec, 0)
}
