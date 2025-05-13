package models

import "encoding/json"

type ZigbeeDevice struct {
	FriendlyName string     `json:"friendly_name"`
	IEEEAddress  string     `json:"ieee_address"`
	Type         string     `json:"type"`
	Manufacturer string     `json:"manufacturer"`
	ModelID      string     `json:"model_id"`
	Definition   Definition `json:"definition"`
	ExposesData  map[string]interface{}
}
type Definition struct {
	Description string   `json:"description"`
	Exposes     []Expose `json:"exposes"`
}
type Expose struct {
	Type        string      `json:"type"`
	Name        string      `json:"name"`
	Property    string      `json:"property,omitempty"`
	Access      int64       `json:"access,omitempty"`
	Description string      `json:"description,omitempty"`
	Unit        string      `json:"unit,omitempty"`
	ValueMax    float64     `json:"value_max,omitempty"`
	ValueMin    float64     `json:"value_min,omitempty"`
	ValueStep   float64     `json:"value_step,omitempty"`
	Values      interface{} `json:"values,omitempty"`
}

type Schedule struct {
	ID          int    `json:"id"`
	DeviceID    int    `json:"device_id"`
	IEEEName    string `json:"ieee_name"`
	Command     string `json:"command"`
	CommandData string `json:"command_data"`
	TimeMark    string `json:"time_mark"`
	CronTime    string `json:"cron_time"`
	Expose      Expose `json:"expose"`
}

func NewSchedule(ieeeName, command, commandData, timeMark, cronTime string) *Schedule {
	var expose Expose
	err := json.Unmarshal([]byte(command), &expose)
	if err != nil {
		return nil
	}
	return &Schedule{IEEEName: ieeeName, Command: command, CommandData: commandData, TimeMark: timeMark, CronTime: cronTime, Expose: expose}
}

//type Scenario struct {
//	ID             int            `json:"id"`
//	DeviceID       int            `json:"device_id"`
//	Property       string         `json:"property"`
//	Operator       string         `json:"operator"` // e.g. >, <, ==
//	Value          string         `json:"value"`    // сравниваемое значение
//	ActionDeviceID int            `json:"action_device_id"`
//	ActionPayload  map[string]any `json:"action_payload"` // JSON объект с командой
//}
