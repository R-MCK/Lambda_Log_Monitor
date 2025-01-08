package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
)

// LogGroupSearch defines a log group and search string
type LogGroupSearch struct {
	LogGroupName string `json:"logGroupName"`
	SearchString string `json:"searchString"`
}

// Event defines the input structure for the Lambda function
type Event struct {
	MonitoredLogGroups []LogGroupSearch `json:"monitoredLogGroups"`
}

// Handler is the Lambda function entry point
func handler(ctx context.Context, event Event) (string, error) {
	// Load AWS configuration
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1")) // Change region as needed
	if err != nil {
		log.Fatalf("Unable to load AWS SDK config: %v", err)
	}

	// Create CloudWatch Logs and CloudWatch clients
	cwlClient := cloudwatchlogs.NewFromConfig(cfg)
	cwClient := cloudwatch.NewFromConfig(cfg)

	var totalMatches int

	for _, logGroup := range event.MonitoredLogGroups {
		// Process each log group
		matches, err := processLogGroup(ctx, cwlClient, cwClient, logGroup)
		if err != nil {
			log.Printf("Error processing log group %s: %v", logGroup.LogGroupName, err)
			continue
		}
		totalMatches += matches
	}

	return fmt.Sprintf("Processed %d log groups, found %d total matches", len(event.MonitoredLogGroups), totalMatches), nil
}

// processLogGroup searches a log group for matching log events
func processLogGroup(ctx context.Context, cwlClient *cloudwatchlogs.Client, cwClient *cloudwatch.Client, logGroup LogGroupSearch) (int, error) {
	endTime := time.Now().UnixMilli()
	startTime := endTime - (12 * 60 * 60 * 1000) // Look back 12 hours

	// Configure log event filtering
	input := &cloudwatchlogs.FilterLogEventsInput{
		LogGroupName:  aws.String(logGroup.LogGroupName),
		FilterPattern: aws.String(logGroup.SearchString),
		StartTime:     aws.Int64(startTime),
		EndTime:       aws.Int64(endTime),
	}

	var matchingEvents []string
	paginator := cloudwatchlogs.NewFilterLogEventsPaginator(cwlClient, input)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return 0, fmt.Errorf("error fetching log events: %v", err)
		}

		for _, event := range page.Events {
			matchingEvents = append(matchingEvents, aws.ToString(event.Message))
		}
	}

	log.Printf("Found %d matching log events in log group %s", len(matchingEvents), logGroup.LogGroupName)

	// Publish metric for the number of matches
	err := publishMetric(ctx, cwClient, logGroup.LogGroupName, len(matchingEvents))
	if err != nil {
		log.Printf("Error publishing metric for log group %s: %v", logGroup.LogGroupName, err)
	}

	return len(matchingEvents), nil
}

// publishMetric publishes custom metrics to CloudWatch
func publishMetric(ctx context.Context, cwClient *cloudwatch.Client, logGroupName string, matches int) error {
	_, err := cwClient.PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
		Namespace: aws.String("Quaemon/Errors"), // Adjust namespace if needed
		MetricData: []types.MetricDatum{
			{
				MetricName: aws.String("ErrorCount"),
				Dimensions: []types.Dimension{
					{
						Name:  aws.String("LogGroupName"),
						Value: aws.String(logGroupName),
					},
				},
				Value: aws.Float64(float64(matches)),
				Unit:  types.StandardUnitCount,
			},
		},
	})
	if err != nil {
		return fmt.Errorf("error publishing metric: %v", err)
	}
	log.Printf("Metric published for log group %s with %d matches", logGroupName, matches)
	return nil
}

func main() {
	lambda.Start(handler)
}
