package util

import (
	"encoding/json"
	"fmt"
)

func ConvertStringToFloat64(data string) (float64, error) {
	var result float64
	err := json.Unmarshal([]byte(data), &result)
	if err != nil {
		return 0, fmt.Errorf("error converting data to float64: %w", err)
	}
	return result, nil
}
