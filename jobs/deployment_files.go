package jobs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	deployer "github.com/hyperpilotio/deployer/apis"
	"github.com/spf13/viper"
)

type DeploymentFiles interface {
	DownloadDeployment(fileName string) (*deployer.Deployment, error)
}

type S3DeploymentFiles struct {
	bucketName string
	awsId      string
	awsSecret  string
	region     string
}

func NewDeploymentFiles(config *viper.Viper) (DeploymentFiles, error) {
	deployments := config.Sub("deployments")
	if deployments.IsSet("s3") {
		s3Config := deployments.GetStringMapString("s3")
		return &S3DeploymentFiles{
			bucketName: s3Config["bucketname"],
			awsId:      s3Config["awsid"],
			awsSecret:  s3Config["awssecret"],
			region:     s3Config["region"],
		}, nil
	}

	return nil, errors.New("No supported deployments type found")
}

func (files *S3DeploymentFiles) DownloadDeployment(fileUrl string) (*deployer.Deployment, error) {
	url, err := url.Parse(fileUrl)
	if err != nil {
		return nil, errors.New("Unable to parse file url: " + err.Error())
	}

	creds := credentials.NewStaticCredentials(files.awsId, files.awsSecret, "")
	config := &aws.Config{
		Region: aws.String(files.region),
	}
	config = config.WithCredentials(creds)
	sess, err := session.NewSession(config)
	if err != nil {
		return nil, errors.New("Unable to create aws session: " + err.Error())
	}

	downloader := s3manager.NewDownloader(sess)
	tmpFile, err := ioutil.TempFile("", "deployment")
	if err != nil {
		return nil, errors.New("Unable to create temp file: " + err.Error())
	}

	defer os.Remove(tmpFile.Name())

	_, err = downloader.Download(tmpFile,
		&s3.GetObjectInput{
			Bucket: aws.String(files.bucketName),
			Key:    aws.String(url.Path),
		})
	if err != nil {
		return nil, fmt.Errorf("Unable to download %s: %v", fileUrl, err)
	}

	tmpFileData, err := ioutil.ReadAll(tmpFile)
	if err != nil {
		return nil, fmt.Errorf("Unable to read temp file: %s", err.Error())
	}

	var deployment deployer.Deployment
	json.Unmarshal(tmpFileData, &deployment)

	return &deployment, nil
}
