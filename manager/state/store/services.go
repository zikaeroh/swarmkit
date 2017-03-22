package store

import (
	"strings"

	"github.com/docker/swarmkit/api"
	memdb "github.com/hashicorp/go-memdb"
)

const tableService = "service"

func init() {
	register(ObjectStoreConfig{
		Table: &memdb.TableSchema{
			Name: tableService,
			Indexes: map[string]*memdb.IndexSchema{
				indexID: {
					Name:    indexID,
					Unique:  true,
					Indexer: api.ServiceIndexerByID{},
				},
				indexName: {
					Name:    indexName,
					Unique:  true,
					Indexer: api.ServiceIndexerByName{},
				},
				indexNetwork: {
					Name:         indexNetwork,
					AllowMissing: true,
					Indexer:      serviceIndexerByNetwork{},
				},
				indexSecret: {
					Name:         indexSecret,
					AllowMissing: true,
					Indexer:      serviceIndexerBySecret{},
				},
				indexCustom: {
					Name:         indexCustom,
					Indexer:      api.ServiceCustomIndexer{},
					AllowMissing: true,
				},
			},
		},
		Save: func(tx ReadTx, snapshot *api.StoreSnapshot) error {
			var err error
			snapshot.Services, err = FindServices(tx, All)
			return err
		},
		Restore: func(tx Tx, snapshot *api.StoreSnapshot) error {
			services, err := FindServices(tx, All)
			if err != nil {
				return err
			}
			for _, s := range services {
				if err := DeleteService(tx, s.ID); err != nil {
					return err
				}
			}
			for _, s := range snapshot.Services {
				if err := CreateService(tx, s); err != nil {
					return err
				}
			}
			return nil
		},
		ApplyStoreAction: func(tx Tx, sa api.StoreAction) error {
			switch v := sa.Target.(type) {
			case *api.StoreAction_Service:
				obj := v.Service
				switch sa.Action {
				case api.StoreActionKindCreate:
					return CreateService(tx, obj)
				case api.StoreActionKindUpdate:
					return UpdateService(tx, obj)
				case api.StoreActionKindRemove:
					return DeleteService(tx, obj.ID)
				}
			}
			return errUnknownStoreAction
		},
	})
}

// CreateService adds a new service to the store.
// Returns ErrExist if the ID is already taken.
func CreateService(tx Tx, s *api.Service) error {
	// Ensure the name is not already in use.
	if tx.lookup(tableService, indexName, strings.ToLower(s.Spec.Annotations.Name)) != nil {
		return ErrNameConflict
	}

	return tx.create(tableService, s)
}

// UpdateService updates an existing service in the store.
// Returns ErrNotExist if the service doesn't exist.
func UpdateService(tx Tx, s *api.Service) error {
	// Ensure the name is either not in use or already used by this same Service.
	if existing := tx.lookup(tableService, indexName, strings.ToLower(s.Spec.Annotations.Name)); existing != nil {
		if existing.GetID() != s.ID {
			return ErrNameConflict
		}
	}

	return tx.update(tableService, s)
}

// DeleteService removes a service from the store.
// Returns ErrNotExist if the service doesn't exist.
func DeleteService(tx Tx, id string) error {
	return tx.delete(tableService, id)
}

// GetService looks up a service by ID.
// Returns nil if the service doesn't exist.
func GetService(tx ReadTx, id string) *api.Service {
	s := tx.get(tableService, id)
	if s == nil {
		return nil
	}
	return s.(*api.Service)
}

// FindServices selects a set of services and returns them.
func FindServices(tx ReadTx, by By) ([]*api.Service, error) {
	checkType := func(by By) error {
		switch by.(type) {
		case byName, byNamePrefix, byIDPrefix, byReferencedNetworkID, byReferencedSecretID, byCustom, byCustomPrefix:
			return nil
		default:
			return ErrInvalidFindBy
		}
	}

	serviceList := []*api.Service{}
	appendResult := func(o api.StoreObject) {
		serviceList = append(serviceList, o.(*api.Service))
	}

	err := tx.find(tableService, by, checkType, appendResult)
	return serviceList, err
}

type serviceIndexerByNetwork struct{}

func (si serviceIndexerByNetwork) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (si serviceIndexerByNetwork) FromObject(obj interface{}) (bool, [][]byte, error) {
	s := obj.(*api.Service)

	var networkIDs [][]byte

	specNetworks := s.Spec.Task.Networks

	if len(specNetworks) == 0 {
		specNetworks = s.Spec.Networks
	}

	for _, na := range specNetworks {
		// Add the null character as a terminator
		networkIDs = append(networkIDs, []byte(na.Target+"\x00"))
	}

	return len(networkIDs) != 0, networkIDs, nil
}

type serviceIndexerBySecret struct{}

func (si serviceIndexerBySecret) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (si serviceIndexerBySecret) FromObject(obj interface{}) (bool, [][]byte, error) {
	s := obj.(*api.Service)

	container := s.Spec.Task.GetContainer()
	if container == nil {
		return false, nil, nil
	}

	var secretIDs [][]byte

	for _, secretRef := range container.Secrets {
		// Add the null character as a terminator
		secretIDs = append(secretIDs, []byte(secretRef.SecretID+"\x00"))
	}

	return len(secretIDs) != 0, secretIDs, nil
}
