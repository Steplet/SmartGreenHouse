package models

import (
	"context"
	"database/sql"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Process struct {
	Database *sql.DB
	Client   mqtt.Client
	Ctx      context.Context
}
type ChartData struct {
	TimeMark time.Time `json:"time_mark"`
	Value    float64   `json:"value"`
}

type Scenario struct {
	ID                 int                    `json:"id"`
	IEEENameInitDevice string                 `json:"ieee_name_init_device"`
	DeviceID           int                    `json:"device_id"`
	ExposesProperty    string                 `json:"exposes_property"`
	Operator           string                 `json:"operator"`
	ExposesValue       string                 `json:"exposes_value"`
	PublishTopic       string                 `json:"publish_topic"`
	ActionPayload      map[string]interface{} `json:"action_payload"`
}

func NewProcess(db *sql.DB, client mqtt.Client, ctx context.Context) *Process {
	return &Process{Database: db, Client: client, Ctx: ctx}
}

func NewScenario(ieeeName, exposeProperty, operator, exposeValue, publishTopic string, actionPayload map[string]interface{}) *Scenario {
	return &Scenario{IEEENameInitDevice: ieeeName, ExposesProperty: exposeProperty, Operator: operator, ExposesValue: exposeValue,
		PublishTopic: publishTopic, ActionPayload: actionPayload}

}
