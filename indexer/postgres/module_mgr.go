package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"cosmossdk.io/schema"
)

type moduleManager struct {
	moduleName   string
	schema       schema.ModuleSchema
	tables       map[string]*TableManager
	definedEnums map[string]schema.EnumDefinition
}

func newModuleManager(moduleName string, modSchema schema.ModuleSchema) *moduleManager {
	return &moduleManager{
		moduleName:   moduleName,
		schema:       modSchema,
		tables:       map[string]*TableManager{},
		definedEnums: map[string]schema.EnumDefinition{},
	}
}

func (m *moduleManager) Init(ctx context.Context, tx *sql.Tx) error {
	// create enum types
	for _, typ := range m.schema.ObjectTypes {
		err := m.createEnumTypesForFields(ctx, tx, typ.KeyFields)
		if err != nil {
			return err
		}

		err = m.createEnumTypesForFields(ctx, tx, typ.ValueFields)
		if err != nil {
			return err
		}
	}

	// create tables for all object types
	for _, typ := range m.schema.ObjectTypes {
		tm := NewTableManager(m.moduleName, typ)
		m.tables[typ.Name] = tm
		err := tm.CreateTable(ctx, tx)
		if err != nil {
			return fmt.Errorf("failed to create table for %s in module %s: %w", typ.Name, m.moduleName, err)
		}
	}

	// create foreign key constraints

	return nil

}