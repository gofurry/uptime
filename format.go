package uptime

import "strconv"

func itoa(v int) string {
	return strconv.Itoa(v)
}

func itoa64(v int64) string {
	return strconv.FormatInt(v, 10)
}

func strconvFormatFloat(v float64, precision int) string {
	return strconv.FormatFloat(v, 'f', precision, 64)
}
