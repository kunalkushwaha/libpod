// +build !remoteclient

package adapter

import (
	"fmt"

	"github.com/containers/libpod/cmd/podman/cliconfig"
	"github.com/containers/libpod/libpod/image"
	"github.com/pkg/errors"
)

// Tree ...
func (r *LocalRuntime) Tree(c *cliconfig.TreeValues) (*image.InfoImage, map[string]*image.LayerInfo, *ContainerImage, error) {
	img, err := r.NewImageFromLocal(c.InputArgs[0])
	if err != nil {
		return nil, nil, nil, err
	}

	// Fetch map of image-layers, which is used for printing output.
	layerInfoMap, err := image.GetLayersMapWithImageInfo(r.Runtime.ImageRuntime())
	if err != nil {
		return nil, nil, nil, errors.Wrapf(err, "error while retrieving layers of image %q", img.InputName)
	}

	// Create an imageInfo and fill the image and layer info
	imageInfo := &image.InfoImage{
		ID:   img.ID(),
		Tags: img.Names(),
	}

	if err := image.BuildImageHierarchyMap(imageInfo, layerInfoMap, img.TopLayer()); err != nil {
		return nil, nil, nil, err
	}
	return imageInfo, layerInfoMap, img, nil
}

// UmountImage removes container(s) based on CLI inputs.
func (r *LocalRuntime) UmountImage(cli *cliconfig.UmountValues) ([]string, map[string]error, error) {
	var (
		ok       = []string{}
		failures = map[string]error{}
	)
	fmt.Println("unmounting... ", cli.InputArgs[0])
	_, err := r.Runtime.ImageRuntime().UnmountImage(cli.InputArgs[0], cli.Force)
	if err != nil {
		return ok, failures, err
	}
	ok = append(ok, cli.InputArgs[0])
	return ok, failures, nil
}
