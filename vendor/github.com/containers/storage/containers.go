package storage

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/containers/storage/pkg/idtools"
	"github.com/containers/storage/pkg/ioutils"
	"github.com/containers/storage/pkg/stringid"
	"github.com/containers/storage/pkg/truncindex"
	"github.com/json-iterator/go"
	digest "github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
)

// A Container is a reference to a read-write layer with metadata.
type Container struct {
	// ID is either one which was specified at create-time, or a random
	// value which was generated by the library.
	ID string `json:"id"`

	// Names is an optional set of user-defined convenience values.  The
	// container can be referred to by its ID or any of its names.  Names
	// are unique among containers.
	Names []string `json:"names,omitempty"`

	// ImageID is the ID of the image which was used to create the container.
	ImageID string `json:"image"`

	// LayerID is the ID of the read-write layer for the container itself.
	// It is assumed that the image's top layer is the parent of the container's
	// read-write layer.
	LayerID string `json:"layer"`

	// Metadata is data we keep for the convenience of the caller.  It is not
	// expected to be large, since it is kept in memory.
	Metadata string `json:"metadata,omitempty"`

	// BigDataNames is a list of names of data items that we keep for the
	// convenience of the caller.  They can be large, and are only in
	// memory when being read from or written to disk.
	BigDataNames []string `json:"big-data-names,omitempty"`

	// BigDataSizes maps the names in BigDataNames to the sizes of the data
	// that has been stored, if they're known.
	BigDataSizes map[string]int64 `json:"big-data-sizes,omitempty"`

	// BigDataDigests maps the names in BigDataNames to the digests of the
	// data that has been stored, if they're known.
	BigDataDigests map[string]digest.Digest `json:"big-data-digests,omitempty"`

	// Created is the datestamp for when this container was created.  Older
	// versions of the library did not track this information, so callers
	// will likely want to use the IsZero() method to verify that a value
	// is set before using it.
	Created time.Time `json:"created,omitempty"`

	// UIDMap and GIDMap are used for setting up a container's root
	// filesystem for use inside of a user namespace where UID mapping is
	// being used.
	UIDMap []idtools.IDMap `json:"uidmap,omitempty"`
	GIDMap []idtools.IDMap `json:"gidmap,omitempty"`

	Flags map[string]interface{} `json:"flags,omitempty"`
}

// ContainerStore provides bookkeeping for information about Containers.
type ContainerStore interface {
	FileBasedStore
	MetadataStore
	BigDataStore
	FlaggableStore

	// Create creates a container that has a specified ID (or generates a
	// random one if an empty value is supplied) and optional names,
	// based on the specified image, using the specified layer as its
	// read-write layer.
	// The maps in the container's options structure are recorded for the
	// convenience of the caller, nothing more.
	Create(id string, names []string, image, layer, metadata string, options *ContainerOptions) (*Container, error)

	// SetNames updates the list of names associated with the container
	// with the specified ID.
	SetNames(id string, names []string) error

	// Get retrieves information about a container given an ID or name.
	Get(id string) (*Container, error)

	// Exists checks if there is a container with the given ID or name.
	Exists(id string) bool

	// Delete removes the record of the container.
	Delete(id string) error

	// Wipe removes records of all containers.
	Wipe() error

	// Lookup attempts to translate a name to an ID.  Most methods do this
	// implicitly.
	Lookup(name string) (string, error)

	// Containers returns a slice enumerating the known containers.
	Containers() ([]Container, error)
}

type containerStore struct {
	lockfile   Locker
	dir        string
	containers []*Container
	idindex    *truncindex.TruncIndex
	byid       map[string]*Container
	bylayer    map[string]*Container
	byname     map[string]*Container
}

func copyContainer(c *Container) *Container {
	return &Container{
		ID:             c.ID,
		Names:          copyStringSlice(c.Names),
		ImageID:        c.ImageID,
		LayerID:        c.LayerID,
		Metadata:       c.Metadata,
		BigDataNames:   copyStringSlice(c.BigDataNames),
		BigDataSizes:   copyStringInt64Map(c.BigDataSizes),
		BigDataDigests: copyStringDigestMap(c.BigDataDigests),
		Created:        c.Created,
		UIDMap:         copyIDMap(c.UIDMap),
		GIDMap:         copyIDMap(c.GIDMap),
		Flags:          copyStringInterfaceMap(c.Flags),
	}
}

func (c *Container) MountLabel() string {
	if label, ok := c.Flags["MountLabel"].(string); ok {
		return label
	}
	return ""
}

func (c *Container) ProcessLabel() string {
	if label, ok := c.Flags["ProcessLabel"].(string); ok {
		return label
	}
	return ""
}

func (r *containerStore) Containers() ([]Container, error) {
	containers := make([]Container, len(r.containers))
	for i := range r.containers {
		containers[i] = *copyContainer(r.containers[i])
	}
	return containers, nil
}

func (r *containerStore) containerspath() string {
	return filepath.Join(r.dir, "containers.json")
}

func (r *containerStore) datadir(id string) string {
	return filepath.Join(r.dir, id)
}

func (r *containerStore) datapath(id, key string) string {
	return filepath.Join(r.datadir(id), makeBigDataBaseName(key))
}

func (r *containerStore) Load() error {
	var json = jsoniter.ConfigCompatibleWithStandardLibrary
	needSave := false
	rpath := r.containerspath()
	data, err := ioutil.ReadFile(rpath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	containers := []*Container{}
	layers := make(map[string]*Container)
	idlist := []string{}
	ids := make(map[string]*Container)
	names := make(map[string]*Container)
	if err = json.Unmarshal(data, &containers); len(data) == 0 || err == nil {
		idlist = make([]string, 0, len(containers))
		for n, container := range containers {
			idlist = append(idlist, container.ID)
			ids[container.ID] = containers[n]
			layers[container.LayerID] = containers[n]
			for _, name := range container.Names {
				if conflict, ok := names[name]; ok {
					r.removeName(conflict, name)
					needSave = true
				}
				names[name] = containers[n]
			}
		}
	}
	r.containers = containers
	r.idindex = truncindex.NewTruncIndex(idlist)
	r.byid = ids
	r.bylayer = layers
	r.byname = names
	if needSave {
		return r.Save()
	}
	return nil
}

func (r *containerStore) Save() error {
	var json = jsoniter.ConfigCompatibleWithStandardLibrary
	if !r.Locked() {
		return errors.New("container store is not locked")
	}
	rpath := r.containerspath()
	if err := os.MkdirAll(filepath.Dir(rpath), 0700); err != nil {
		return err
	}
	jdata, err := json.Marshal(&r.containers)
	if err != nil {
		return err
	}
	defer r.Touch()
	return ioutils.AtomicWriteFile(rpath, jdata, 0600)
}

func newContainerStore(dir string) (ContainerStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	lockfile, err := GetLockfile(filepath.Join(dir, "containers.lock"))
	if err != nil {
		return nil, err
	}
	lockfile.Lock()
	defer lockfile.Unlock()
	cstore := containerStore{
		lockfile:   lockfile,
		dir:        dir,
		containers: []*Container{},
		byid:       make(map[string]*Container),
		bylayer:    make(map[string]*Container),
		byname:     make(map[string]*Container),
	}
	if err := cstore.Load(); err != nil {
		return nil, err
	}
	return &cstore, nil
}

func (r *containerStore) lookup(id string) (*Container, bool) {
	if container, ok := r.byid[id]; ok {
		return container, ok
	} else if container, ok := r.byname[id]; ok {
		return container, ok
	} else if container, ok := r.bylayer[id]; ok {
		return container, ok
	} else if longid, err := r.idindex.Get(id); err == nil {
		if container, ok := r.byid[longid]; ok {
			return container, ok
		}
	}
	return nil, false
}

func (r *containerStore) ClearFlag(id string, flag string) error {
	container, ok := r.lookup(id)
	if !ok {
		return ErrContainerUnknown
	}
	delete(container.Flags, flag)
	return r.Save()
}

func (r *containerStore) SetFlag(id string, flag string, value interface{}) error {
	container, ok := r.lookup(id)
	if !ok {
		return ErrContainerUnknown
	}
	if container.Flags == nil {
		container.Flags = make(map[string]interface{})
	}
	container.Flags[flag] = value
	return r.Save()
}

func (r *containerStore) Create(id string, names []string, image, layer, metadata string, options *ContainerOptions) (container *Container, err error) {
	if id == "" {
		id = stringid.GenerateRandomID()
		_, idInUse := r.byid[id]
		for idInUse {
			id = stringid.GenerateRandomID()
			_, idInUse = r.byid[id]
		}
	}
	if _, idInUse := r.byid[id]; idInUse {
		return nil, ErrDuplicateID
	}
	names = dedupeNames(names)
	for _, name := range names {
		if _, nameInUse := r.byname[name]; nameInUse {
			return nil, errors.Wrapf(ErrDuplicateName,
				fmt.Sprintf("the container name \"%s\" is already in use by \"%s\". You have to remove that container to be able to reuse that name.", name, r.byname[name].ID))
		}
	}
	if err == nil {
		container = &Container{
			ID:             id,
			Names:          names,
			ImageID:        image,
			LayerID:        layer,
			Metadata:       metadata,
			BigDataNames:   []string{},
			BigDataSizes:   make(map[string]int64),
			BigDataDigests: make(map[string]digest.Digest),
			Created:        time.Now().UTC(),
			Flags:          copyStringInterfaceMap(options.Flags),
			UIDMap:         copyIDMap(options.UIDMap),
			GIDMap:         copyIDMap(options.GIDMap),
		}
		r.containers = append(r.containers, container)
		r.byid[id] = container
		r.idindex.Add(id)
		r.bylayer[layer] = container
		for _, name := range names {
			r.byname[name] = container
		}
		err = r.Save()
		container = copyContainer(container)
	}
	return container, err
}

func (r *containerStore) Metadata(id string) (string, error) {
	if container, ok := r.lookup(id); ok {
		return container.Metadata, nil
	}
	return "", ErrContainerUnknown
}

func (r *containerStore) SetMetadata(id, metadata string) error {
	if container, ok := r.lookup(id); ok {
		container.Metadata = metadata
		return r.Save()
	}
	return ErrContainerUnknown
}

func (r *containerStore) removeName(container *Container, name string) {
	container.Names = stringSliceWithoutValue(container.Names, name)
}

func (r *containerStore) SetNames(id string, names []string) error {
	names = dedupeNames(names)
	if container, ok := r.lookup(id); ok {
		for _, name := range container.Names {
			delete(r.byname, name)
		}
		for _, name := range names {
			if otherContainer, ok := r.byname[name]; ok {
				r.removeName(otherContainer, name)
			}
			r.byname[name] = container
		}
		container.Names = names
		return r.Save()
	}
	return ErrContainerUnknown
}

func (r *containerStore) Delete(id string) error {
	container, ok := r.lookup(id)
	if !ok {
		return ErrContainerUnknown
	}
	id = container.ID
	toDeleteIndex := -1
	for i, candidate := range r.containers {
		if candidate.ID == id {
			toDeleteIndex = i
			break
		}
	}
	delete(r.byid, id)
	r.idindex.Delete(id)
	delete(r.bylayer, container.LayerID)
	for _, name := range container.Names {
		delete(r.byname, name)
	}
	if toDeleteIndex != -1 {
		// delete the container at toDeleteIndex
		if toDeleteIndex == len(r.containers)-1 {
			r.containers = r.containers[:len(r.containers)-1]
		} else {
			r.containers = append(r.containers[:toDeleteIndex], r.containers[toDeleteIndex+1:]...)
		}
	}
	if err := r.Save(); err != nil {
		return err
	}
	if err := os.RemoveAll(r.datadir(id)); err != nil {
		return err
	}
	return nil
}

func (r *containerStore) Get(id string) (*Container, error) {
	if container, ok := r.lookup(id); ok {
		return copyContainer(container), nil
	}
	return nil, ErrContainerUnknown
}

func (r *containerStore) Lookup(name string) (id string, err error) {
	if container, ok := r.lookup(name); ok {
		return container.ID, nil
	}
	return "", ErrContainerUnknown
}

func (r *containerStore) Exists(id string) bool {
	_, ok := r.lookup(id)
	return ok
}

func (r *containerStore) BigData(id, key string) ([]byte, error) {
	if key == "" {
		return nil, errors.Wrapf(ErrInvalidBigDataName, "can't retrieve container big data value for empty name")
	}
	c, ok := r.lookup(id)
	if !ok {
		return nil, ErrContainerUnknown
	}
	return ioutil.ReadFile(r.datapath(c.ID, key))
}

func (r *containerStore) BigDataSize(id, key string) (int64, error) {
	if key == "" {
		return -1, errors.Wrapf(ErrInvalidBigDataName, "can't retrieve size of container big data with empty name")
	}
	c, ok := r.lookup(id)
	if !ok {
		return -1, ErrContainerUnknown
	}
	if c.BigDataSizes == nil {
		c.BigDataSizes = make(map[string]int64)
	}
	if size, ok := c.BigDataSizes[key]; ok {
		return size, nil
	}
	if data, err := r.BigData(id, key); err == nil && data != nil {
		if r.SetBigData(id, key, data) == nil {
			c, ok := r.lookup(id)
			if !ok {
				return -1, ErrContainerUnknown
			}
			if size, ok := c.BigDataSizes[key]; ok {
				return size, nil
			}
		}
	}
	return -1, ErrSizeUnknown
}

func (r *containerStore) BigDataDigest(id, key string) (digest.Digest, error) {
	if key == "" {
		return "", errors.Wrapf(ErrInvalidBigDataName, "can't retrieve digest of container big data value with empty name")
	}
	c, ok := r.lookup(id)
	if !ok {
		return "", ErrContainerUnknown
	}
	if c.BigDataDigests == nil {
		c.BigDataDigests = make(map[string]digest.Digest)
	}
	if d, ok := c.BigDataDigests[key]; ok {
		return d, nil
	}
	if data, err := r.BigData(id, key); err == nil && data != nil {
		if r.SetBigData(id, key, data) == nil {
			c, ok := r.lookup(id)
			if !ok {
				return "", ErrContainerUnknown
			}
			if d, ok := c.BigDataDigests[key]; ok {
				return d, nil
			}
		}
	}
	return "", ErrDigestUnknown
}

func (r *containerStore) BigDataNames(id string) ([]string, error) {
	c, ok := r.lookup(id)
	if !ok {
		return nil, ErrContainerUnknown
	}
	return copyStringSlice(c.BigDataNames), nil
}

func (r *containerStore) SetBigData(id, key string, data []byte) error {
	if key == "" {
		return errors.Wrapf(ErrInvalidBigDataName, "can't set empty name for container big data item")
	}
	c, ok := r.lookup(id)
	if !ok {
		return ErrContainerUnknown
	}
	if err := os.MkdirAll(r.datadir(c.ID), 0700); err != nil {
		return err
	}
	err := ioutils.AtomicWriteFile(r.datapath(c.ID, key), data, 0600)
	if err == nil {
		save := false
		if c.BigDataSizes == nil {
			c.BigDataSizes = make(map[string]int64)
		}
		oldSize, sizeOk := c.BigDataSizes[key]
		c.BigDataSizes[key] = int64(len(data))
		if c.BigDataDigests == nil {
			c.BigDataDigests = make(map[string]digest.Digest)
		}
		oldDigest, digestOk := c.BigDataDigests[key]
		newDigest := digest.Canonical.FromBytes(data)
		c.BigDataDigests[key] = newDigest
		if !sizeOk || oldSize != c.BigDataSizes[key] || !digestOk || oldDigest != newDigest {
			save = true
		}
		addName := true
		for _, name := range c.BigDataNames {
			if name == key {
				addName = false
				break
			}
		}
		if addName {
			c.BigDataNames = append(c.BigDataNames, key)
			save = true
		}
		if save {
			err = r.Save()
		}
	}
	return err
}

func (r *containerStore) Wipe() error {
	ids := make([]string, 0, len(r.byid))
	for id := range r.byid {
		ids = append(ids, id)
	}
	for _, id := range ids {
		if err := r.Delete(id); err != nil {
			return err
		}
	}
	return nil
}

func (r *containerStore) Lock() {
	r.lockfile.Lock()
}

func (r *containerStore) Unlock() {
	r.lockfile.Unlock()
}

func (r *containerStore) Touch() error {
	return r.lockfile.Touch()
}

func (r *containerStore) Modified() (bool, error) {
	return r.lockfile.Modified()
}

func (r *containerStore) IsReadWrite() bool {
	return r.lockfile.IsReadWrite()
}

func (r *containerStore) TouchedSince(when time.Time) bool {
	return r.lockfile.TouchedSince(when)
}

func (r *containerStore) Locked() bool {
	return r.lockfile.Locked()
}
