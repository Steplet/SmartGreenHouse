package mqtt_service

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"SmartGreenHouse/database"
	"SmartGreenHouse/models"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const deviceTopic = "zigbee2mqtt/bridge/devices"

func InitMQTTClient(errChan chan<- string) mqtt.Client {

	brokerURL := "localhost:1883" // По умолчанию, если переменная не задана

	opts := mqtt.NewClientOptions().AddBroker(fmt.Sprintf("tcp://%s", brokerURL))
	opts.SetClientID("go-mqtt-client")
	opts.SetKeepAlive(2 * time.Minute)
	opts.SetPingTimeout(10 * time.Second)
	opts.SetConnectionLostHandler(connectionLostHandler)
	opts.SetOnConnectHandler(connectHandler)

	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		errChan <- fmt.Sprintf("mqtt connect error: %w", token.Error())
		//log.Fatalf("Ошибка подключения к MQTT: %v", token.Error())
	}

	return client
}

var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
	log.Println("Успешно подключились к MQTT брокеру")
}

var connectionLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	log.Printf("Соединение с MQTT брокером потеряно: %v", err)
}

func printDevices(mes []models.ZigbeeDevice) {
	for _, device := range mes {
		fmt.Printf("Устройство: %s (%s), Тип: %s, Производитель: %s, Модель: %s\n, Description: %s\n",
			device.FriendlyName, device.IEEEAddress, device.Type, device.Manufacturer, device.ModelID, device.Definition.Description)
		for _, exp := range device.Definition.Exposes {
			fmt.Printf("Name %s, Description: %s, Type: %s, Property: %s, Access: %d, \n", exp.Name, exp.Description, exp.Type, exp.Property, exp.Access)
		}
	}
}

func SubscribeToDeviceTopic(process *models.Process, errChan chan<- string) {
	go func() {
		process.Client.Subscribe(deviceTopic, 0, handleDevices(process, errChan))

		for {
			select {
			case <-process.Ctx.Done():
				process.Client.Disconnect(250)
				log.Println("MQTT client exit")
				return
			}
		}
	}()
}

func handleDevices(process *models.Process, errChan chan<- string) func(client mqtt.Client, msg mqtt.Message) {
	return func(client mqtt.Client, msg mqtt.Message) {
		var newDevices []models.ZigbeeDevice
		if err := json.Unmarshal(msg.Payload(), &newDevices); err != nil {
			log.Println("Error parsing devices:", err)
			return
		}
		//printDevices(newDevices)

		err := database.SaveDevices(newDevices, process.Database)
		if err != nil {
			log.Println("Save device data error:", err)
			return
		}

		database.DevicesMu.RLock()
		database.Devices = newDevices
		for _, device := range newDevices {
			if _, ok := database.DevMap[device.FriendlyName]; !ok {
				err = database.GetExposesDataFromDevice(&device, process.Database)
				if err != nil {
					log.Println("Getting device data error:", err)
				}
				database.DevMap[device.FriendlyName] = device
				err := listenDevicesData(process, device)
				if err != nil {
					log.Println("Error listening devices:", err)
				}
			}
		}
		database.DevicesMu.RUnlock()
	}

}

func listenDevicesData(process *models.Process, device models.ZigbeeDevice) error {

	process.Client.Subscribe(fmt.Sprintf("zigbee2mqtt/%s", device.FriendlyName), 0, func(client mqtt.Client, msg mqtt.Message) {
		m := map[string]interface{}{}
		err := json.Unmarshal(msg.Payload(), &m)
		if err != nil {
			log.Printf("Error parsing devices: %v", err)
			return
		}
		fmt.Printf("Received message: %v from %s\n", m, msg.Topic())

		device.ExposesData = m
		err = database.SavePublishedDataFromDevice(device, msg.Payload(), process.Database)
		if err != nil {
			log.Println("Save published data from database:", err)
			return
		}
		database.DevMap[device.FriendlyName] = device

		for _, scenario := range database.Scenarios {
			if scenario.IEEENameInitDevice == device.IEEEAddress {
				var m map[string]interface{}
				err := json.Unmarshal(msg.Payload(), &m)
				if err != nil {
					log.Printf("Error parsing devices: %v", err)
				}
				//fmt.Println(m)
				for k, v := range scenario.ActionPayload {
					//fmt.Println(m[k], v)
					if m[k] == v {
						fmt.Println("Got a scenario again")
						return
					}

				}

				payload, err := json.Marshal(scenario.ActionPayload)
				if err != nil {
					log.Println("Error encoding scenario action payload:", err)
					return
				}
				token := process.Client.Publish(scenario.PublishTopic, 0, false, payload)
				token.Wait()
				if token.Error() != nil {
					log.Println("Error publishing scenario action payload:", err)
					return
				}
			}
		}
	})

	log.Printf("Listening for Zigbee Devices %s data by topic %s\n", device.FriendlyName, device.IEEEAddress)
	return nil
}
