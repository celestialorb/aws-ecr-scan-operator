package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/procyon-projects/chrono"
	"github.com/spf13/viper"

	log "github.com/sirupsen/logrus"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type AwsEcrClientKey struct{}

var (
	scansRequested = promauto.NewCounter(prometheus.CounterOpts{
		Name: "aws_ecr_scans_requested",
		Help: "The total count of AWS ECR image scan requests sent.",
	})
	scanRequestErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "aws_ecr_scans_requested_errors",
		Help: "The total count of AWS ECR image scan requests that results in an error.",
	})
	scansRateLimited = promauto.NewCounter(prometheus.CounterOpts{
		Name: "aws_ecr_scans_rate_limited",
		Help: "The total count of AWS ECR image scan requests rejected due to rate-limiting.",
	})
)

func main() {
	// Establish our configuration default values.
	viper.SetDefault("log.format", "logfmt")
	viper.SetDefault("log.level", "info")
	viper.SetDefault("cron.schedule", "0 35 */3 * * *")
	viper.SetDefault("web.host", "127.0.0.1")
	viper.SetDefault("web.port", 2112)
	viper.SetDefault("metrics.path", "/metrics")
	viper.SetEnvPrefix("AWS_ECR_SCANNER")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	// Setup our logging format before we output any log messages.
	switch viper.GetString("log.format") {
	case "json":
		log.SetFormatter(&log.JSONFormatter{})
	case "logfmt":
	case "text":
	default:
		log.SetFormatter(&log.TextFormatter{})
	}

	// Set our logging level before we do any processing.
	level, err := log.ParseLevel(viper.GetString("log.level"))
	if err != nil {
		log.WithFields(log.Fields{
			"level": viper.GetString("log.level"),
		}).Warn("unknown log level, defaulting to INFO level")
		level = log.InfoLevel
	}
	log.SetLevel(level)
	log.Debug("logging initialized")

	// Output the service's configuration in case we need to see it.
	// NOTE: this should never include any sensitive information or secrets.
	log.WithFields(log.Fields{
		"config": viper.AllSettings(),
	}).Info("reconciled configuration")

	// Establish our cron scheduler.
	log.Debug("initializing chrono scheduler")
	scheduler := chrono.NewDefaultTaskScheduler()
	_, err = scheduler.ScheduleWithCron(TriggerScans, viper.GetString("cron.schedule"))
	if err != nil {
		log.WithFields(log.Fields{
			"err": err,
		}).Fatal("failed to initialize chrono scheduler")
	}

	// Add our Prometheus metrics handler.
	log.Debug("adding Prometheus metrics handler")
	http.Handle(viper.GetString("metrics.path"), promhttp.Handler())

	// Start our webserver.
	log.Debug("starting webserver")
	err = http.ListenAndServe(
		fmt.Sprintf(
			"%s:%d",
			viper.GetString("web.host"),
			viper.GetInt32("web.port"),
		),
		nil,
	)
	if err != nil {
		log.WithFields(log.Fields{
			"err": err,
		}).Fatal("webserver failed")
	}
}

func TriggerScans(ctx context.Context) {
	// Reconcile our AWS client configuration.
	log.Debug("loading AWS configuration")
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.WithFields(log.Fields{
			"err": err,
		}).Fatal("failed to load AWS configuration")
	}

	// Create our AWS client object to be injected into the context.
	log.Debug("creating AWS ECR client")
	client := ecr.NewFromConfig(cfg)

	// Create a new context with the AWS ECR client object injected into it.
	ecrctx := context.WithValue(ctx, AwsEcrClientKey{}, client)

	// Create a paginator (TODO)
	log.Debug("describing AWS ECR repositories")
	paginator := ecr.NewDescribeRepositoriesPaginator(
		client,
		&ecr.DescribeRepositoriesInput{},
	)

	// While we still have pages in the DescribeRepositories response, take a page and
	// pass each repository off.
	for paginator.HasMorePages() {
		response, err := paginator.NextPage(ctx)
		if err != nil {
			log.WithFields(log.Fields{
				"err": err,
			}).Fatal("failed to retrieve next page of repositories")
		}

		for _, repository := range response.Repositories {
			go ReconcileRepository(ecrctx, repository)
		}
	}
}

func ReconcileRepository(ctx context.Context, repository types.Repository) {
	// Setup our logging context for the function.
	logger := log.WithFields(log.Fields{
		"repository": *repository.RepositoryName,
	})
	logger.Info("reconciling respository")

	// Retrieve the AWS ECR client from the provided context.
	client := ctx.Value(AwsEcrClientKey{}).(*ecr.Client)

	// Create a paginator for listing images in case we have a lot.
	paginator := ecr.NewListImagesPaginator(client, &ecr.ListImagesInput{
		RepositoryName: repository.RepositoryName,
	})

	// While we still have pages, grab the next one and send off those images to
	// initiate scans against.
	for paginator.HasMorePages() {
		response, err := paginator.NextPage(ctx)
		if err != nil {
			logger.Fatal(err)
		}

		// Start the process to request an image scan against each image.
		for _, image := range response.ImageIds {
			go ReconcileImage(ctx, repository, image)
		}
	}
}

func ReconcileImage(
	ctx context.Context,
	repository types.Repository,
	image types.ImageIdentifier,
) {
	// Retrieve the AWS ECR client from the provided context.
	client := ctx.Value(AwsEcrClientKey{}).(*ecr.Client)

	// Setup our logging context for the function.
	logger := log.WithFields(log.Fields{
		"image": map[string]string{
			"digest": *image.ImageDigest,
			"tag":    *image.ImageTag,
		},
		"repository": *repository.RepositoryName,
	})
	logger.Info("requesting image scan")

	_, err := client.StartImageScan(ctx, &ecr.StartImageScanInput{
		ImageId:        &image,
		RepositoryName: repository.RepositoryName,
	})
	if err != nil {
		// Check for a rate-limiting error, if this is the case we just want to
		// ignore it as we're only allowed to initiate a scan once every
		// twenty-four hours in AWS ECR for an image.
		var lee *types.LimitExceededException
		if errors.As(err, &lee) {
			logger.Info("rate-limiting error detected, skipping image for now")
			scansRateLimited.Inc()
			return
		}

		// Otherwise, ensure the error is observable.
		scanRequestErrors.Inc()
		logger.Fatal(err)
	}

	// Ensure our scan request success is observable.
	scansRequested.Inc()
	logger.WithFields(log.Fields{
		"image": map[string]string{
			"digest": *image.ImageDigest,
			"tag":    *image.ImageTag,
		},
		"repository": *repository.RepositoryName,
	}).Info("scan successfully requested")
}
