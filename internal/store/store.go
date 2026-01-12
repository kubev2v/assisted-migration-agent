package store

import "database/sql"

// Store provides access to all storage repositories.
type Store struct {
	db            *sql.DB
	configuration *ConfigurationStore
	inventory     *InventoryStore
	vm            *VMStore
}

func NewStore(db *sql.DB) *Store {
	return &Store{
		db:            db,
		configuration: NewConfigurationStore(db),
		inventory:     NewInventoryStore(db),
		vm:            NewVMStore(db),
	}
}

func (s *Store) Configuration() *ConfigurationStore {
	return s.configuration
}

func (s *Store) Inventory() *InventoryStore {
	return s.inventory
}

func (s *Store) VM() *VMStore {
	return s.vm
}

func (s *Store) Close() error {
	return s.db.Close()
}
