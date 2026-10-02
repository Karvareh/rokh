package content

import (
	"bytes"
	"fmt"
	"io"

	"rokh/carrier"
)

// Content lives inside the vessel (contract E5): the machine library outside
// the carrier is no longer written. Bring puts the bytes into a recording as
// chunked content records sealed for the readers of address, and returns the
// descriptor that the content.put event of the same recording carries. The
// event and its content land in one commit.
func Bring(r *carrier.Recording, address string, size uint64, open func() (io.Reader, error), mediaType string) (Descriptor, []byte, error) {
	id, err := r.Content(address, size, open)
	if err != nil {
		return Descriptor{}, nil, err
	}
	d := Descriptor{Hash: id, Size: size, Type: mediaType}
	payload, err := d.Encode()
	if err != nil {
		return Descriptor{}, nil, err
	}
	return d, payload, nil
}

// BringBytes is Bring for content already in memory.
func BringBytes(r *carrier.Recording, address string, body []byte, mediaType string) (Descriptor, []byte, error) {
	return Bring(r, address, uint64(len(body)), func() (io.Reader, error) { return bytes.NewReader(body), nil }, mediaType)
}

// Fetch writes the content a descriptor names, from inside the carrier, only
// after every chunk has opened and the whole has hashed to the descriptor
// (E8). Nothing is written to w before that.
func Fetch(c *carrier.Carrier, d Descriptor, w io.Writer) error {
	var buf bytes.Buffer
	n, err := c.ContentToSized(d.Hash, d.Size, &buf)
	if err != nil {
		return err
	}
	if n != d.Size {
		return fmt.Errorf("%w: %d bytes, the descriptor says %d", ErrShape, n, d.Size)
	}
	_, err = w.Write(buf.Bytes())
	return err
}
