package util

import (
	"math"
)

// Contains checks if a slice contains a specific string
func Contains(slice []string, val string) bool {
	for _, item := range slice {
		if item == val {
			return true
		}
	}
	return false
}

// ConvertBytesToMB converts bytes to megabytes safely
func ConvertBytesToMB(bytes int64) int64 {
	return bytes / (1024 * 1024)
}

// IntPtr returns a pointer to the given int
func IntPtr(i int) *int {
	return &i
}

// FloatPtr returns a pointer to the given float
func FloatPtr(i float64) *float64 {
	return &i
}

// Round Method to round to 2 decimals
func Round(f float64) float64 {
	return math.Round(f*100) / 100
}

// BytesToTB converts a value in bytes to terabytes (TB).
func BytesToTB[T ~int | ~int64 | ~float64](bytes T) float64 {
	return float64(bytes) / 1024.0 / 1024.0 / 1024.0 / 1024.0
}

// BytesToGB converts a value in bytes to gigabytes (GB).
func BytesToGB[T ~int | ~int64 | ~float64](bytes T) int {
	return int(math.Round(float64(bytes) / 1024.0 / 1024.0 / 1024.0))
}

// GBToTB converts a value in gigabytes (GB) to terabytes (TB).
func GBToTB[T ~int | ~int64 | ~float64](gb T) float64 {
	return float64(gb) / 1024.0
}

// MBToGB converts a value in MB to GB.
func MBToGB[T ~int | ~int32 | ~float64](mb T) int {
	return int(math.Round(float64(mb) / 1024.0))
}
