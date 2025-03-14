// Copyright (c) Microsoft Corporation.
// Licensed under the MIT License.

package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/microsoft/confidential-sidecars/pkg/attest"
	"github.com/microsoft/confidential-sidecars/pkg/common"
	"github.com/microsoft/confidential-sidecars/pkg/skr"
	"github.com/sirupsen/logrus"
)

type AzureInfo struct {
	Identity  common.Identity  `json:"identity,omitempty"`
	CertCache attest.CertCache `json:"certcache,omitempty"`
}

type RemoteFilesystemsInformation struct {
	AzureInfo        AzureInfo         `json:"azure_info"`
	AzureFilesystems []AzureFilesystem `json:"azure_filesystems"`
}

// AzureFilesystem contains information about a filesystem image stored in Azure
// Blob Storage.
type AzureFilesystem struct {
	// This is the URL of the image
	AzureUrl string `json:"azure_url"`
	// This is a private AzureUrl
	AzureUrlPrivate bool `json:"azure_url_private"`
	// This is the path where the filesystem will be exposed in the container.
	MountPoint string `json:"mount_point"`
	// This is the information used by skr to release the encryption key of the filesystem
	KeyBlob skr.KeyBlob `json:"key,omitempty"`
	// This is a testing key hexstring encoded to be used against the filesystem. This should
	// be used only for testing.
	RawKeyHexString string `json:"raw_key, omitempty"`
}

func usage() {
	fmt.Printf("Usage of %s:\n", os.Args[0])
	flag.PrintDefaults()
}

func main() {
	base64string := flag.String("base64", "", "base64-encoded json string with all information")
	logLevel := flag.String("loglevel", "debug", "Logging Level: trace, debug, info, warning, error, fatal, panic.")
	logFile := flag.String("logfile", "", "Logging Target: An optional file name/path. Omit for console output.")

	flag.Usage = usage

	flag.Parse()

	if *logFile != "" {
		// If the file doesn't exist, create it. If it exists, append to it.
		file, err := os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
		if err != nil {
			logrus.Fatal(err)
		}
		defer file.Close()
		logrus.SetOutput(file)
	}

	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logrus.Fatal(err)
	}
	logrus.SetLevel(level)

	logrus.Infof("Starting %s...", os.Args[0])

	logrus.Infof("Args:")
	logrus.Debugf("   Log Level: %s", *logLevel)
	logrus.Debugf("   Log File:  %s", *logFile)
	logrus.Debugf("   base64:    %s", *base64string)

	logrus.Infof("Creating temporary directory")
	tempDir, err := ioutil.TempDir("", "remotefs")
	if err != nil {
		logrus.Fatalf("Failed to create temp dir: %s", err.Error())
	}
	logrus.Infof("Temporary directory: %s", tempDir)

	// Decode information
	bytes, err := base64.StdEncoding.DecodeString(*base64string)
	if err != nil {
		logrus.Fatalf("Failed to decode base64: %s", err.Error())
	}

	info := RemoteFilesystemsInformation{}
	err = json.Unmarshal(bytes, &info)
	if err != nil {
		logrus.Fatalf("Failed to unmarshal: %s", err.Error())
	}

	// populate missing attributes in KeyBlob
	for i, _ := range info.AzureFilesystems {
		// set the api versions and the tee type for which the authority will authorize secure key release
		info.AzureFilesystems[i].KeyBlob.MHSM.APIVersion = "api-version=7.3-preview"
		info.AzureFilesystems[i].KeyBlob.Authority.APIVersion = "api-version=2020-10-01"
		info.AzureFilesystems[i].KeyBlob.Authority.TEEType = "SevSnpVM"
	}

	logrus.Debugf("JSON = %+v", info)

	err = MountAzureFilesystems(tempDir, info)
	if err != nil {
		logrus.Fatalf("Failed to mount filesystems: %s", err.Error())
	}

	os.Exit(0)
}
