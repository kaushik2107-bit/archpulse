package kernel

import "fmt"

type World struct{ resources []SimResource }

func (w *World) Reserve() ResourceID {
	id := ResourceID(len(w.resources))
	w.resources = append(w.resources, nil)
	return id
}

func (w *World) Set(id ResourceID, resource SimResource) error {
	if int(id) >= len(w.resources) {
		return fmt.Errorf("resource id %d is not reserved", id)
	}
	if resource == nil {
		return fmt.Errorf("resource id %d cannot be nil", id)
	}
	w.resources[id] = resource
	return nil
}

func (w *World) Get(id ResourceID) SimResource {
	if int(id) >= len(w.resources) {
		return nil
	}
	return w.resources[id]
}

func (w *World) Len() int { return len(w.resources) }
