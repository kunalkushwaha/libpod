package main

import (
	"context"
	"fmt"

	"github.com/containers/libpod/cmd/podman/libpodruntime"
	"github.com/pkg/errors"
	"github.com/urfave/cli"
)

var (
	treeDescription = "Displays the dependent layers of an image. The information is printed in tree format"

	treeCommand = cli.Command{
		Name:                   "tree",
		Usage:                  "Show dependent layers of a specified image in tree format",
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

	out, err := image.Tree(context.Background(), args[0])
	if err != nil {
		return errors.Wrapf(err, "error getting dependencies of image %q", image.InputName)
	}
	fmt.Println(out)

	return nil
}
