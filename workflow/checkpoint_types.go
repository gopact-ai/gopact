package workflow

import (
	"encoding/gob"
	"fmt"
	"reflect"
	"sort"
	"sync"
)

var checkpointTypeRegisterMu sync.Mutex

func workflowCheckpointTypes[I, O any](wf *Workflow[I, O]) ([]reflect.Type, error) {
	types := make(map[reflect.Type]struct{})
	add := func(typ reflect.Type) {
		if typ != nil && typ.Kind() != reflect.Interface {
			types[typ] = struct{}{}
		}
	}
	add(typeOf[I]())
	add(typeOf[O]())
	add(wf.contextType)
	for _, node := range wf.nodes {
		add(node.inputType())
		add(node.outputType())
	}
	for _, typ := range wf.checkpointTypes {
		if typ.Kind() == reflect.Interface {
			return nil, fmt.Errorf("workflow: checkpoint type %s must be concrete", typ)
		}
		add(typ)
	}
	ordered := make([]reflect.Type, 0, len(types))
	for typ := range types {
		ordered = append(ordered, typ)
	}
	sort.Slice(ordered, func(i, j int) bool { return checkpointTypeIdentity(ordered[i]) < checkpointTypeIdentity(ordered[j]) })
	return ordered, nil
}

func registerWorkflowCheckpointTypes(types []reflect.Type) error {
	checkpointTypeRegisterMu.Lock()
	defer checkpointTypeRegisterMu.Unlock()
	for _, typ := range types {
		if err := registerCheckpointType(typ); err != nil {
			return err
		}
	}
	return nil
}

func registerCheckpointType(typ reflect.Type) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("workflow: register checkpoint type %s: %v", typ, recovered)
		}
	}()
	gob.Register(reflect.Zero(typ).Interface())
	return nil
}

func checkpointTypeIdentity(typ reflect.Type) string {
	if typ.Name() != "" {
		if typ.PkgPath() != "" {
			return typ.PkgPath() + "." + typ.Name()
		}
		return typ.Name()
	}
	switch typ.Kind() {
	case reflect.Pointer:
		return "*" + checkpointTypeIdentity(typ.Elem())
	case reflect.Slice:
		return "[]" + checkpointTypeIdentity(typ.Elem())
	case reflect.Array:
		return fmt.Sprintf("[%d]%s", typ.Len(), checkpointTypeIdentity(typ.Elem()))
	case reflect.Map:
		return "map[" + checkpointTypeIdentity(typ.Key()) + "]" + checkpointTypeIdentity(typ.Elem())
	default:
		return typ.String()
	}
}
