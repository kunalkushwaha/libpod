package main

import (
	"fmt"

	"github.com/containers/libpod/cmd/podman/libpodruntime"
	"github.com/containers/libpod/libpod/image"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

// treeTemplateParams stores info about each layer
type treeTemplateParams struct {
	ID   string
	Size string
	Tags []string
}

var (
	treeDescription = "Displays the dependent layers of an image. The information is printed as tree format"

	treeCommand = cli.Command{
		Name:                   "tree",
		Usage:                  "Show dependent images of a specified image",
		Description:            treeDescription,
		Action:                 treeCmd,
		ArgsUsage:              "",
		UseShortOptionHandling: true,
	}
)

func treeCmd(c *cli.Context) error {

	runtime, err := libpodruntime.GetRuntime(c)
	if err != nil {
		return errors.Wrapf(err, "could not get runtime")
	}
	defer runtime.Shutdown(false)

	args := c.Args()
	if len(args) == 0 {
		return errors.Errorf("an image name must be specified")
	}
	if len(args) > 1 {
		return errors.Errorf("podman tree takes at most 1 argument")
	}

	image, err := runtime.ImageRuntime().NewFromLocal(args[0])
	if err != nil {
		return err
	}

	imageInfo, err := image.Tree(getContext(), args[0])
	if err != nil {
		return errors.Wrapf(err, "error getting history of image %q", image.InputName)
	}

	return generateTreeOutput(imageInfo)

	//image.GetRootImage()
	//GetChildrenList
	//BuildTree
	//PrintTree

	/*_, err = image.Tree2(context.Background())
	if err != nil {
		return errors.Wrapf(err, "error getting history of image %q", image.InputName)
	}*/

	return nil
}

// generateTreeOutput generates the history based on the format given
func generateTreeOutput(history []image.TreeImage) error {
	if len(history) == 0 {
		return nil
	}
	fmt.Println("length of history : ", len(history))
	//TODO:
	//Build Tree structure out of layer info of image
	for _, h := range history {
		fmt.Printf("ID: %s, ParentID: %s, OriginalID: %s, Size: %v, Details : %s \n", h.ID, h.ParentID, h.OrigID, h.Size, h.RepoTags)
	}

	return nil
}
