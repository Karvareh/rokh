package vesselstore

import (
	"fmt"
	"sort"
	"time"

	"rokh/frame"
	"rokh/turn"
	"rokh/vessel"
)

// Vessel is Records on a real vessel: every object and pointer is one record
// of the home's spaces in the same vessel as the home's ledger, written in one
// commit under the writer's turn of the ledger's folder.
type Vessel struct {
	V   *vessel.Vessel
	Dir string // the vessel's folder, whose turn every commit takes
}

func (r Vessel) commit(fill func(*vessel.Tx) error) error {
	lock, err := turn.Acquire(r.Dir, 15*time.Second)
	if err != nil {
		return err
	}
	defer lock.Release()
	tx, err := r.V.Begin(lock)
	if err != nil {
		return err
	}
	if err := fill(tx); err != nil {
		tx.Abandon()
		return err
	}
	out, err := tx.CommitGrowing()
	switch out {
	case vessel.Recorded:
		return nil
	case vessel.Unknown:
		return fmt.Errorf("%w: %v", ErrUnknown, err)
	}
	return fmt.Errorf("vesselstore: not recorded: %v", err)
}

// PutObject stores every chunk of one object in one commit.
func (r Vessel) PutObject(space byte, name [32]byte, chunks []Chunk) error {
	return r.commit(func(tx *vessel.Tx) error {
		for _, c := range chunks {
			h := vessel.Header{Type: vessel.RecObject, Space: space, Name: frame.ID(name),
				Chunk: c.Chunk, Chunks: c.Chunks, Size: c.Size}
			if err := tx.Put(h, c.Envelope); err != nil {
				return err
			}
		}
		return nil
	})
}

// Object reads every stored chunk of one object.
func (r Vessel) Object(space byte, name [32]byte) ([]Chunk, error) {
	var out []Chunk
	var refs []vessel.Ref
	var hs []vessel.Header
	err := r.V.Scan(func(h vessel.Header, ref vessel.Ref) error {
		if h.Type == vessel.RecObject && h.Space == space && h.Name == frame.ID(name) {
			hs = append(hs, h)
			refs = append(refs, ref)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for i, h := range hs {
		env, err := r.V.Body(refs[i])
		if err != nil {
			return nil, err
		}
		out = append(out, Chunk{Chunk: h.Chunk, Chunks: h.Chunks, Size: h.Size, Envelope: env})
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Chunk < out[j].Chunk })
	return out, nil
}

// SetPointer records a pointer; the latest commit wins.
func (r Vessel) SetPointer(space byte, name [32]byte, env []byte) error {
	return r.commit(func(tx *vessel.Tx) error {
		return tx.Put(vessel.Header{Type: vessel.RecPointer, Space: space, Name: frame.ID(name)}, env)
	})
}

// Pointer reads the latest record of a pointer.
func (r Vessel) Pointer(space byte, name [32]byte) ([]byte, bool, error) {
	var last *vessel.Ref
	err := r.V.Scan(func(h vessel.Header, ref vessel.Ref) error {
		if h.Type == vessel.RecPointer && h.Space == space && h.Name == frame.ID(name) {
			rr := ref
			last = &rr
		}
		return nil
	})
	if err != nil || last == nil {
		return nil, false, err
	}
	env, err := r.V.Body(*last)
	if err != nil {
		return nil, false, err
	}
	return env, true, nil
}
