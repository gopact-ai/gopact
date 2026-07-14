package workflow

import (
	"encoding/gob"
	"fmt"
	"reflect"
	"sort"
	"sync"
)

var (
	checkpointTypeRegisterMu sync.Mutex
	checkpointTypesByName    = make(map[string]reflect.Type)
	checkpointTypesByBase    = make(map[reflect.Type]checkpointTypeRegistration)
)

type checkpointTypeRegistration struct {
	typ      reflect.Type
	wireName string
}

type checkpointTypePlan struct {
	checkpointTypeRegistration
	base reflect.Type
}

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
	plan, err := preflightCheckpointTypes(types)
	if err != nil {
		return err
	}
	for _, registration := range plan {
		if err := registerCheckpointType(registration.typ); err != nil {
			return err
		}
		recordCheckpointType(registration)
	}
	return nil
}

func preflightCheckpointTypes(types []reflect.Type) ([]checkpointTypePlan, error) {
	byName := make(map[string]reflect.Type, len(checkpointTypesByName)+len(types))
	for name, typ := range checkpointTypesByName {
		byName[name] = typ
	}
	byBase := make(map[reflect.Type]checkpointTypeRegistration, len(checkpointTypesByBase)+len(types))
	for base, registration := range checkpointTypesByBase {
		byBase[base] = registration
	}
	plan := make([]checkpointTypePlan, 0, len(types))
	for _, typ := range types {
		wireName := checkpointGobName(typ)
		if existing, ok := byName[wireName]; ok && existing != typ {
			return nil, fmt.Errorf("%w: wire name %q maps to different concrete types", ErrCheckpointTypeConflict, wireName)
		}
		base, err := checkpointBaseType(typ)
		if err != nil {
			return nil, err
		}
		if existing, ok := byBase[base]; ok && (existing.typ != typ || existing.wireName != wireName) {
			return nil, fmt.Errorf(
				"%w: base type %s cannot use both %q and %q",
				ErrCheckpointTypeConflict,
				base,
				existing.wireName,
				wireName,
			)
		}
		byName[wireName] = typ
		registration := checkpointTypeRegistration{typ: typ, wireName: wireName}
		byBase[base] = registration
		plan = append(plan, checkpointTypePlan{checkpointTypeRegistration: registration, base: base})
	}
	return plan, nil
}

func recordCheckpointType(registration checkpointTypePlan) {
	checkpointTypesByName[registration.wireName] = registration.typ
	checkpointTypesByBase[registration.base] = registration.checkpointTypeRegistration
}

func registerCheckpointType(typ reflect.Type) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%w: register checkpoint type %s: %v", ErrCheckpointTypeConflict, typ, recovered)
		}
	}()
	gob.Register(reflect.Zero(typ).Interface())
	return nil
}

func checkpointBaseType(typ reflect.Type) (reflect.Type, error) {
	visited := make(map[reflect.Type]struct{})
	for typ.Kind() == reflect.Pointer {
		if _, ok := visited[typ]; ok {
			return nil, fmt.Errorf("%w: recursive pointer type %s", ErrCheckpointTypeConflict, typ)
		}
		visited[typ] = struct{}{}
		typ = typ.Elem()
	}
	return typ, nil
}

// checkpointGobName mirrors encoding/gob.Register, including its historical
// pointer naming behavior. Changing it would make preflight disagree with gob.
func checkpointGobName(typ reflect.Type) string {
	runtimeType := typ
	name := runtimeType.String()
	star := ""
	if runtimeType.Name() == "" {
		if pointerType := runtimeType; pointerType.Kind() == reflect.Pointer {
			star = "*"
			// Deliberately preserve encoding/gob's historical bug: runtimeType
			// remains the pointer instead of becoming pointerType.Elem().
			runtimeType = pointerType
		}
	}
	if runtimeType.Name() != "" {
		if runtimeType.PkgPath() == "" {
			name = star + runtimeType.Name()
		} else {
			name = star + runtimeType.PkgPath() + "." + runtimeType.Name()
		}
	}
	return name
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
