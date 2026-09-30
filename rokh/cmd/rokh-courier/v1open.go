package main

import (
	"crypto/rand"

	"rokh/carrier"
	"rokh/key"
	"rokh/medium"
	"rokh/vessel"
)

// openOwner opens a v1 carrier with the owner's passphrase: the owner's cell
// gives the session that opens every record, and the owner's reader.
func openOwner(dir, pass string) (*carrier.Carrier, key.Secret, error) {
	c, _, err := carrier.Open(medium.Dir{Root: dir}, vessel.Unlock(key.Unlock(pass)), rand.Reader, nil)
	if err != nil {
		return nil, key.Secret{}, err
	}
	v := c.Vessel()
	info := v.Info()
	sess, sec, err := key.VesselSession(pass, v.Slots(), info.Salt, info.Iter, v.SharedKey(), rand.Reader)
	if err != nil {
		return nil, key.Secret{}, err
	}
	c.SetSealer(sess)
	return c, sec, nil
}
