// Administrative storage operations. Requests and credentials arrive through stdin.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/julia-elikhis/romana-learning/internal/filestore"
)

type settings struct {
	Provider, Bucket, Prefix, Endpoint, Region, AccessKey, SecretKey string
}

func open(ctx context.Context, s settings) (filestore.Store, error) {
	switch s.Provider {
	case "gcs":
		return filestore.NewGCS(ctx, s.Bucket, s.Prefix)
	case "s3":
		return filestore.NewS3(s.Endpoint, s.Bucket, s.Prefix, s.Region, s.AccessKey, s.SecretKey)
	}
	return nil, errors.New("unsupported provider")
}

func run() error {
	var request struct {
		Action         string
		Source, Target settings
		Objects        []filestore.Original
	}
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	target, err := open(ctx, request.Target)
	if err != nil {
		return err
	}
	defer target.Close()
	switch request.Action {
	case "check":
		if err := filestore.CheckAccess(ctx, target); err != nil {
			return err
		}
	case "sync":
		source, err := open(ctx, request.Source)
		if err != nil {
			return err
		}
		defer source.Close()
		if err := filestore.SyncOriginals(ctx, source, target, request.Objects); err != nil {
			return err
		}
	case "verify":
		if err := filestore.SyncOriginals(ctx, target, target, request.Objects); err != nil {
			return err
		}
	case "delete":
		for _, object := range request.Objects {
			if err := filestore.RemoveOriginal(ctx, target, object.Key); err != nil {
				return err
			}
		}
	default:
		return errors.New("unknown storage operation")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]int{"verifiedObjects": len(request.Objects)})
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Storage operation failed; check credentials and original-file integrity.")
		os.Exit(1)
	}
}
