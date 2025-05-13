package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"SmartGreenHouse/models"
	"SmartGreenHouse/util"

	_ "github.com/lib/pq"
)

var (
	DevicesMu   sync.RWMutex
	Devices     []models.ZigbeeDevice
	DevMap      = make(map[string]models.ZigbeeDevice)
	ScenariosMu sync.RWMutex
	Scenarios   []models.Scenario
	ScenarioMap = make(map[int]models.Scenario)
)

func InitDB(ctx context.Context, errChan chan<- string) *sql.DB {
	// Параметры подключения к серверу PostgreSQL
	connStr := "user=postgres password=steplet host=localhost port=5432 sslmode=disable"
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		errChan <- fmt.Sprintf("could not connect to database: %v", err)
	}
	log.Println("Connected to database init")
	dbName := "greenhouse"
	var exists bool

	err = db.QueryRow(`SELECT EXISTS(
  SELECT 1 FROM pg_database WHERE datname = $1
 )`, dbName).Scan(&exists)

	if err != nil {
		errChan <- fmt.Sprintf("could not check if database exists: %v", err)
	}

	if !exists {
		_, err = db.Exec(`CREATE DATABASE ` + dbName + ";")
		if err != nil {
			log.Println("Error creating database:", err)
			errChan <- fmt.Sprintf("could not create database %s: %v", dbName, err)
		}
		log.Printf("Created database %s", dbName)
	}

	db.Close()

	//check db greenhouse
	connStr = fmt.Sprintf("user=postgres password=steplet host=localhost port=5432 dbname=%s sslmode=disable", dbName)
	db, err = sql.Open("postgres", connStr)
	if err != nil {
		errChan <- fmt.Sprintf("could not connect to database: %v", err)
	}
	log.Printf("Database %s exists", dbName)

	go func() {
		for {
			select {
			case <-ctx.Done():
				db.Close()
				log.Println("Database exit")
				return
			}
		}
	}()

	log.Println("Connected to database")
	err = CreateDBTables(db)
	if err != nil {
		errChan <- fmt.Sprintf("could not create tables: %v", err)
	}

	return db
}

func CreateDBTables(db *sql.DB) error {
	var createTablesQuery = `
	CREATE TABLE IF NOT EXISTS definitions (
    id SERIAL PRIMARY KEY,
    description TEXT NOT NULL
);

	CREATE TABLE IF NOT EXISTS exposes (
    id SERIAL PRIMARY KEY, -- Уникальный идентификатор характеристики, автоинкремент
    definition_id INTEGER NOT NULL REFERENCES definitions(id) ON DELETE CASCADE, -- Внешний ключ, ссылающийся на таблицу definitions.
                                                                                -- ON DELETE CASCADE означает, что при удалении определения удалятся все связанные с ним характеристики.

    -- Поля из структуры Expose
    type TEXT NOT NULL, -- Тип характеристики (e.g., 'light', 'numeric', 'enum')
    name TEXT NOT NULL, -- Имя характеристики (e.g., 'state', 'brightness', 'temperature')

    -- Поля с тегом omitempty в Go, могут быть NULL в базе данных
    property TEXT, -- Имя свойства, если отличается от name
    access BIGINT, -- Уровень доступа (e.g., 1=GET, 2=SET, 3=GET/SET)
    description TEXT, -- Описание характеристики
    unit TEXT, -- Единица измерения (e.g., 'C', '%')
    value_max DOUBLE PRECISION, -- Максимальное значение для числовых типов
    value_min DOUBLE PRECISION, -- Минимальное значение для числовых типов
    value_step DOUBLE PRECISION, -- Шаг изменения значения для числовых типов

    -- []interface{} хранится как JSONB
    values JSONB -- Список возможных значений для типа enum (e.g., ['on', 'off']), или другие произвольные данные.
                -- JSONB - это бинарный формат JSON, оптимизированный для хранения и запросов.
);	
-- 
CREATE TABLE IF NOT EXISTS exposes_data (
--     
	id SERIAL PRIMARY KEY,
	device_id INTEGER REFERENCES zigbee_devices(id) ON DELETE CASCADE,
	time_mark timestamp NOT NULL,
	exposes_data_json JSONB
);

CREATE TABLE IF NOT EXISTS zigbee_devices (
    
    -- Поля из структуры ZigbeeDevice
    id SERIAL PRIMARY KEY,
    ieee_address TEXT NOT NULL, -- IEEE адрес устройства как первичный ключ (уникальный идентификатор)
    friendly_name TEXT NOT NULL, -- Человекочитаемое имя устройства
    type_dev TEXT NOT NULL, -- Тип устройства (e.g., 'Router', 'EndDevice')
    manufacturer TEXT NOT NULL, -- Производитель
    model_id TEXT NOT NULL, -- Идентификатор модели

--     Внешний ключ, ссылающийся на определение устройства
--     Может быть NULL, если определение еще не загружено или недоступно
    definition_id INTEGER REFERENCES definitions(id) ON DELETE CASCADE-- ON DELETE SET NULL означает, что если определение удалено,
--                                                                         поле definition_id в устройстве станет NULL, но само устройство останется.
--    exposes_data_id INTEGER REFERENCES exposes_data(id) ON DELETE SET NULL-- Произвольные данные о характеристиках устройства, которые могут меняться динамически.
);

CREATE TABLE IF NOT EXISTS schedule (
    id SERIAL PRIMARY KEY,
    device_id INTEGER REFERENCES zigbee_devices(id) ON DELETE CASCADE,
    command JSONB NOT NULL,
    command_data TEXT NOT NULL,
    time_mark timestamp NOT NULL
);

CREATE TABLE IF NOT EXISTS scenarios (
    id SERIAL PRIMARY KEY,
    device_id INTEGER REFERENCES zigbee_devices(id) ON DELETE CASCADE,
    property TEXT NOT NULL,
    operator TEXT NOT NULL,
    value_comp TEXT NOT NULL,
--     action_device_id INTEGER REFERENCES zigbee_devices(id) ON DELETE CASCADE,
    publish_topic TEXT NOT NULL,
    action_payload JSONB NOT NULL,
    created_at TIMESTAMP DEFAULT now()
);

`
	_, err := db.Exec(createTablesQuery)
	if err != nil {
		log.Println(err)
		return fmt.Errorf("could not create table definitions: %v", err)
	}
	log.Println("Tables created or already exist.")

	return nil
}

func SaveDevices(devices []models.ZigbeeDevice, db *sql.DB) error {
	log.Println("Saving device data...")

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("не удалось начать транзакцию: %w", err)
	}
	// Откладываем откат транзакции на случай ошибки.
	// tx.Rollback() безопасен для вызова даже если Commit() был успешен.
	defer tx.Rollback()
	var deviceDefinitionID int

	for _, device := range devices {
		var exist int
		err = db.QueryRow(`
		SELECT COUNT(*)
		FROM zigbee_devices
		WHERE ieee_address = $1
		`, device.IEEEAddress).Scan(&exist)
		if err != nil {
			return fmt.Errorf("error while querying zigbee_devices: %w", err)
		}

		if exist != 0 {
			log.Println("Device already exists.")
			continue
		}

		err = db.QueryRow(`
		INSERT INTO definitions
		(description)		
		VALUES ($1) RETURNING id`,
			device.Definition.Description).Scan(&deviceDefinitionID)

		if err != nil {
			return fmt.Errorf("error saving device definitions data in database: %w", err)
		}
		_, err = db.Exec(`
		INSERT INTO zigbee_devices 
		(ieee_address, friendly_name, type_dev, manufacturer, model_id, definition_id)
		VALUES ($1, $2, $3, $4, $5, $6)`,
			device.IEEEAddress, device.Type, device.Manufacturer,
			device.ModelID, device.Definition.Description, deviceDefinitionID)

		if err != nil {
			return fmt.Errorf("error saving device zigbee data in database: %w", err)
		}

		for _, exp := range device.Definition.Exposes {
			exposeValues, err := json.Marshal(exp.Values)
			if err != nil {
				return fmt.Errorf("error marshaling device exposes data : %w", err)
			}
			_, err = db.Exec(`
			INSERT INTO exposes 
			(definition_id, type, name, property, access, description, unit, value_max, value_min, value_step, values)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
				deviceDefinitionID, exp.Type, exp.Name, exp.Property, exp.Access, exp.Description, exp.Unit, exp.ValueMax,
				exp.ValueMin, exp.ValueStep, exposeValues)

			if err != nil {
				log.Printf("error saving exposes data in database: %s", exp.Values)
				return fmt.Errorf("error saving device exposes data in database: %w", err)
			}
		}

	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("error commit transaction in database: %w", err)
	}

	log.Println("Saved device data in database.")

	return nil
}

func SavePublishedDataFromDevice(device models.ZigbeeDevice, payload []byte, db *sql.DB) error {
	log.Printf("Saving published data from device: %s\n", device.FriendlyName)
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("error saving published data from database: %w", err)
	}
	defer tx.Rollback()

	var zigbeeDeviceID int
	err = db.QueryRow(`
	SELECT id from zigbee_devices WHERE ieee_address = $1
	`, device.IEEEAddress).Scan(&zigbeeDeviceID)
	if err != nil {
		return fmt.Errorf("error saving published data from database: %w", err)
	}

	_, err = db.Exec(`
	INSERT INTO exposes_data
	(device_id, time_mark, exposes_data_json)
	VALUES ($1, $2, $3)
	`, zigbeeDeviceID, time.Now(), payload)
	if err != nil {
		return fmt.Errorf("error saving published data from database: %w", err)
	}
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("error saving published data from database: %w", err)
	}
	log.Printf("Saved published data from device: %s\n", device.FriendlyName)

	return nil
}

func SaveScheduleData(schedule *models.Schedule, db *sql.DB) (int, error) {
	log.Printf("Saving scheduled data from device: %s\n", schedule.IEEEName)
	var deviceID int
	err := db.QueryRow(`
	SELECT id FROM zigbee_devices WHERE ieee_address = $1
	`, schedule.IEEEName).Scan(&deviceID)
	if err != nil {
		return -1, fmt.Errorf("error find id while saving scheduled data from device: %w", err)
	}
	var scheduleID int
	err = db.QueryRow(`
	INSERT INTO schedule
	(device_id, command, command_data, time_mark)
	VALUES ($1, $2, $3, $4) RETURNING id
	`, deviceID, schedule.Command, schedule.CommandData, schedule.TimeMark).Scan(&scheduleID)
	if err != nil {
		return -1, fmt.Errorf("error saving scheduled data from device: %w", err)
	}

	log.Printf("End saving scheduled data from device: %s\n", schedule.IEEEName)

	return scheduleID, nil
}

func GetSchedules(db *sql.DB) ([]models.Schedule, error) {
	log.Printf("Getting scheduled data from database\n")

	rows, err := db.Query(`
	Select id, device_id, command, command_data, time_mark FROM schedule 
	`)
	if err != nil {
		return nil, fmt.Errorf("error getting scheduled data from database: %w", err)
	}
	defer rows.Close()
	var result []models.Schedule
	for rows.Next() {
		var data models.Schedule
		err = rows.Scan(&data.ID, &data.DeviceID, &data.Command, &data.CommandData, &data.TimeMark)
		if err != nil {
			return nil, fmt.Errorf("error getting scheduled data from database: %w", err)
		}
		var ieeeName string
		err = db.QueryRow(`Select ieee_address FROM zigbee_devices WHERE id = $1`, data.DeviceID).Scan(&ieeeName)
		if err != nil {
			return nil, fmt.Errorf("error getting scheduled data from database: %w", err)
		}
		data.IEEEName = ieeeName
		var exp models.Expose
		err = json.Unmarshal([]byte(data.Command), &exp)
		if err != nil {
			return nil, fmt.Errorf("error unmarshaling scheduled data from database: %w", err)
		}
		data.Expose = exp
		result = append(result, data)
	}
	log.Printf("End getting scheduled data from device\n")

	return result, nil
}

func GetExposesDataFromDevice(device *models.ZigbeeDevice, db *sql.DB) error {
	log.Printf("Getting exposes data from Tabel: %s\n", device.FriendlyName)
	var jsonData []byte
	var deviceID int

	err := db.QueryRow(`
	SELECT id FROM zigbee_devices Where ieee_address = $1
	`, device.IEEEAddress).Scan(&deviceID)
	if err != nil {
		return fmt.Errorf("error getting id dvice from database: %w", err)
	}
	err = db.QueryRow(`
	SELECT exposes_data_json FROM exposes_data WHERE device_id = $1 ORDER BY time_mark DESC LIMIT 1
	`, deviceID).Scan(&jsonData)
	if err != nil {
		return fmt.Errorf("error getting exposes data from database: %w", err)
	}
	m := map[string]interface{}{}
	err = json.Unmarshal(jsonData, &m)
	if err != nil {
		return fmt.Errorf("error unmarshaling exposes data from database: %w", err)
	}

	device.ExposesData = m

	log.Printf("End Getting exposes data from Table: %s\n", device.FriendlyName)

	return nil
}

func GetDataForChartByAction(device models.ZigbeeDevice, action string, db *sql.DB) ([]models.ChartData, error) {
	log.Printf("Getting data from database by action %s for chart\n", action)
	var deviceID int
	err := db.QueryRow(`
	SELECT id FROM zigbee_devices WHERE ieee_address = $1
	`, device.IEEEAddress).Scan(&deviceID)
	if err != nil {
		return nil, fmt.Errorf("error getting id from zigbee_devoces: %w", err)
	}

	rows, err := db.Query(`
	SELECT time_mark, exposes_data_json FROM exposes_data
	WHERE device_id = $1 ORDER BY time_mark DESC limit 20
	`, deviceID)
	if err != nil {
		return nil, fmt.Errorf("error getting rows from database by id: %w", err)
	}

	defer rows.Close()
	var result []models.ChartData
	for rows.Next() {
		var data models.ChartData
		var rawJson []byte
		var jsonData map[string]interface{}

		if err = rows.Scan(&data.TimeMark, &rawJson); err != nil {
			return nil, fmt.Errorf("error getting data from database in struct: %w", err)
		}
		err = json.Unmarshal(rawJson, &jsonData)
		//fmt.Println(jsonData)

		switch jsonData[action].(type) {
		case float64:
			//log.Println("Got a float64 for chart")
			data.Value = jsonData[action].(float64) //TODO
		case string:
			//log.Println("Got a string for chart")
			valueFloat64, err := util.ConvertStringToFloat64(jsonData[action].(string))
			if err != nil {
				return nil, fmt.Errorf("error converting float64: %w", err)
			}
			data.Value = valueFloat64
		default:
			return nil, fmt.Errorf("error getting data for float: %w", err)
		}
		result = append(result, data)
	}

	log.Printf("End getting data from database by action %s for chart\n", action)

	return result, nil
}

func DeleteSchedule(id string, db *sql.DB) error {
	log.Printf("Deleting scheduled data from database\n")
	_, err := db.Exec(`DELETE FROM schedule WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("error deleting scheduled data from database: %w", err)
	}
	log.Printf("End deleting scheduled data from database\n")

	return nil
}

func SaveScenario(scenario models.Scenario, db *sql.DB) (int, error) {
	log.Printf("Saving scenario data to database\n")
	var deviceID int
	err := db.QueryRow(`
	SELECT id FROM zigbee_devices WHERE ieee_address = $1
	`, scenario.IEEENameInitDevice).Scan(&deviceID)
	if err != nil {
		return -1, fmt.Errorf("error find id while saving scenario data from device: %w", err)
	}
	payload, err := json.Marshal(scenario.ActionPayload)
	if err != nil {
		log.Println("Error marshalling payload:", err)
		return -1, fmt.Errorf("error marshalling payload: %w", err)
	}

	var scenarioID int
	err = db.QueryRow(`
	INSERT INTO scenarios
	(device_id, property, operator, value_comp, publish_topic, action_payload)
	VALUES ($1, $2, $3, $4, $5, $6) RETURNING id
	`, deviceID, scenario.ExposesProperty, scenario.Operator, scenario.ExposesValue, scenario.PublishTopic,
		payload).Scan(&scenarioID)
	if err != nil {
		return -1, fmt.Errorf("error saving scheduled data from device: %w", err)
	}
	scenario.ID = scenarioID
	ScenariosMu.RLock()
	Scenarios = append(Scenarios, scenario)
	ScenariosMu.RUnlock()

	log.Println("End saving scenario data to database")

	return scenarioID, nil
}

func GetScenarios(db *sql.DB) ([]models.Scenario, error) {
	log.Printf("Getting scenarios data from database\n")

	rows, err := db.Query(`
	Select id, device_id, property, operator, value_comp, publish_topic, action_payload FROM scenarios 
	`)
	if err != nil {
		return nil, fmt.Errorf("error getting scenario data from database: %w", err)
	}
	defer rows.Close()
	var result []models.Scenario
	for rows.Next() {
		var data models.Scenario
		var rawJson []byte
		err = rows.Scan(&data.ID, &data.DeviceID, &data.ExposesProperty, &data.Operator, &data.ExposesValue, &data.PublishTopic, &rawJson)
		if err != nil {
			return nil, fmt.Errorf("error getting scenario data from database: %w", err)
		}
		err = json.Unmarshal(rawJson, &data.ActionPayload)
		if err != nil {
			return nil, fmt.Errorf("error getting scenario data from database: %w", err)
		}

		var ieeeName string
		err = db.QueryRow(`Select ieee_address FROM zigbee_devices WHERE id = $1`, data.DeviceID).Scan(&ieeeName)
		if err != nil {
			return nil, fmt.Errorf("error getting scheduled data from database: %w", err)
		}
		data.IEEENameInitDevice = ieeeName

		result = append(result, data)
	}
	ScenariosMu.RLock()
	Scenarios = result
	ScenariosMu.RUnlock()

	log.Printf("End getting scenarios data from database\n")

	return result, nil
}

func DeleteScenario(id string, db *sql.DB) error {
	log.Printf("Deleting scenario data from database\n")
	_, err := db.Exec(`DELETE FROM scenarios WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("error deleting scenario data from database: %w", err)
	}
	_, err = GetScenarios(db)
	if err != nil {
		return fmt.Errorf("error getting scenario data from database: %w", err)
	}
	log.Printf("End scenario scheduled data from database\n")

	return nil
}
