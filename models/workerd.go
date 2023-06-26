package models

import (
	"errors"
	"os"
	"path/filepath"
	"vorker/conf"
	"vorker/defs"
	"vorker/entities"
	"vorker/rpc"
	"vorker/utils"

	"vorker/utils/database"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	"gorm.io/gorm"
)

type Worker struct {
	gorm.Model
	*entities.Worker
}

func init() {
	db := database.GetDB()
	defer database.CloseDB(db)
	db.AutoMigrate(&Worker{})
}

func (w *Worker) TableName() string {
	return "workers"
}

func GetWorkerByUID(userID uint, uid string) (*Worker, error) {
	var worker Worker
	db := database.GetDB()
	defer database.CloseDB(db)
	if err := db.Where(&Worker{
		Worker: &entities.Worker{
			UserID: uint64(userID),
		},
	}).Where(
		&Worker{
			Worker: &entities.Worker{
				UID: uid,
			},
		},
	).First(&worker).Error; err != nil {
		return nil, err
	}
	return &worker, nil
}

func GetWorkersByNames(userID uint, names []string) ([]*Worker, error) {
	var workers []*Worker
	db := database.GetDB()
	defer database.CloseDB(db)
	if err := db.Where(&Worker{
		Worker: &entities.Worker{
			UserID: uint64(userID),
		},
	}).Where("name in (?)", names).Find(&workers).Error; err != nil {
		return nil, err
	}
	return workers, nil
}

func AdminGetWorkersByNames(names []string) ([]*Worker, error) {
	var workers []*Worker
	db := database.GetDB()
	defer database.CloseDB(db)
	if err := db.Where("name in (?)", names).Find(&workers).Error; err != nil {
		return nil, err
	}
	return workers, nil
}

func GetAllWorkers(userID uint) ([]*Worker, error) {
	var workers []*Worker
	db := database.GetDB()
	defer database.CloseDB(db)
	if err := db.Where(&Worker{
		Worker: &entities.Worker{
			UserID: uint64(userID),
		},
	}).Find(&workers).Error; err != nil {
		return nil, err
	}
	return workers, nil
}

func AdminGetAllWorkers() ([]*Worker, error) {
	var workers []*Worker
	db := database.GetDB()
	defer database.CloseDB(db)
	if err := db.Find(&workers).Error; err != nil {
		return nil, err
	}
	return workers, nil
}

func AdminGetAllWorkersTunnelMap() (map[string]string, error) {
	workers, err := AdminGetAllWorkers()
	if err != nil {
		return nil, err
	}
	tunnelMap := make(map[string]string)
	for _, worker := range workers {
		tunnelMap[worker.Name] = worker.TunnelID
	}
	return tunnelMap, nil
}

func AdminGetWorkersByNodeName(nodeName string) ([]*Worker, error) {
	var workers []*Worker
	db := database.GetDB()
	defer database.CloseDB(db)
	if err := db.Where(&Worker{
		Worker: &entities.Worker{
			NodeName: nodeName,
		},
	}).Find(&workers).Error; err != nil {
		return nil, err
	}
	return workers, nil
}

func GetWorkers(userID uint, offset, limit int) ([]*Worker, error) {
	var workers []*Worker
	db := database.GetDB()
	defer database.CloseDB(db)
	if err := db.Where(&Worker{
		Worker: &entities.Worker{
			UserID: uint64(userID),
		},
	}).Offset(offset).Limit(limit).Find(&workers).Error; err != nil {
		return nil, err
	}
	return workers, nil
}

func Trans2Entities(workers []*Worker) []*entities.Worker {
	var entities []*entities.Worker
	for _, worker := range workers {
		entities = append(entities, worker.Worker)
	}
	return entities
}

func (w *Worker) Create() error {
	if w.NodeName == conf.AppConfigInstance.NodeName {
		AddGost(w.TunnelID, w.Name, w.Port)
		err := w.UpdateFile()
		if err != nil {
			return err
		}
	} else {
		n, err := GetNodeByNodeName(w.NodeName)
		if err != nil {
			return err
		}
		wp, err := proto.Marshal(w)
		if err != nil {
			return err
		}
		go rpc.EventNotify(n.Node, defs.EventAddWorker, map[string][]byte{
			defs.KeyWorkerProto: wp,
		})
	}

	db := database.GetDB()
	defer database.CloseDB(db)
	return db.Create(w).Error
}

func (w *Worker) Update() error {
	if w.NodeName == conf.AppConfigInstance.NodeName {
		DeleteGost(w.Name)
		AddGost(w.TunnelID, w.Name, w.Port)
		err := w.UpdateFile()
		if err != nil {
			return err
		}
	}
	db := database.GetDB()
	defer database.CloseDB(db)
	return db.Save(w).Error
}

func (w *Worker) Delete() error {
	if w.NodeName == conf.AppConfigInstance.NodeName {
		DeleteGost(w.Name)
	} else {
		n, err := GetNodeByNodeName(w.NodeName)
		if err != nil {
			return err
		}
		wp, err := proto.Marshal(w)
		if err != nil {
			return err
		}
		go rpc.EventNotify(n.Node, defs.EventDeleteWorker, map[string][]byte{
			defs.KeyWorkerProto: wp,
		})
	}
	db := database.GetDB()
	defer database.CloseDB(db)
	if err := db.Where(&Worker{
		Worker: &entities.Worker{
			UID: w.UID,
		},
	}).Unscoped().Delete(w).Error; err != nil {
		return err
	}

	return w.DeleteFile()
}

func (w *Worker) Flush() error {
	port, err := utils.GetAvailablePort(defs.DefaultHostName)
	if err != nil {
		return err
	}
	if len(w.TunnelID) == 0 {
		w.TunnelID = uuid.New().String()
	}

	if err = w.DeleteFile(); err != nil {
		return err
	}

	w.Port = int32(port)
	if err = w.Update(); err != nil {
		return err
	}
	return nil
}

func (w *Worker) DeleteFile() error {
	return os.RemoveAll(
		filepath.Join(
			conf.AppConfigInstance.WorkerdDir,
			defs.WorkerCodePath,
			w.UID,
		),
	)
}

func (w *Worker) UpdateFile() error {
	return utils.WriteFile(
		filepath.Join(
			conf.AppConfigInstance.WorkerdDir,
			defs.WorkerCodePath,
			w.UID,
			w.Entry),
		string(w.Code))
}

func SyncWorkers(workerList *entities.WorkerList) error {
	partialFail := false
	UIDs := []string{}
	oldWorkers, err := AdminGetAllWorkers()
	if err != nil {
		return err
	}

	for _, worker := range workerList.Workers {
		modelWorker := &Worker{Worker: worker}
		UIDs = append(UIDs, worker.UID)

		if err := modelWorker.Delete(); err != nil && err != gorm.ErrRecordNotFound {
			logrus.WithError(err).Errorf("sync workers db delete error, worker is: %+v", worker)
			partialFail = true
			continue
		}

		if err := modelWorker.Create(); err != nil {
			logrus.WithError(err).Errorf("sync workers db create error, worker is: %+v", worker)
			partialFail = true
			continue
		}

		if err := modelWorker.DeleteFile(); err != nil {
			logrus.WithError(err).Errorf("sync workers delete file error, worker is: %+v", worker)
			partialFail = true
			continue
		}

		if err := modelWorker.UpdateFile(); err != nil {
			logrus.WithError(err).Errorf("sync workers update file error, worker is: %+v", worker)
			partialFail = true
			continue
		}
	}

	// delete workers that not in workerList
	for _, worker := range oldWorkers {
		if !utils.ContainsString(UIDs, worker.UID) {
			if err := worker.Delete(); err != nil {
				logrus.WithError(err).Errorf("sync workers delete worker error, worker is: %+v", worker)
				partialFail = true
				continue
			}
			if err := worker.DeleteFile(); err != nil {
				logrus.WithError(err).Errorf("sync workers delete file error, worker is: %+v", worker)
				partialFail = true
				continue
			}
		}
	}

	if partialFail {
		return errors.New("partial fail")
	}

	return nil
}
