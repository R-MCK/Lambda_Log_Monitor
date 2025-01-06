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
	LogGroupName string `json:"logGroupName"` // ARN or name of the log group
	SearchString string `json:"searchString"` // Search pattern (e.g., "ERROR", "connection failed")
}

// Event defines the input structure for the Lambda function
type Event struct {
	MonitoredLogGroups []LogGroupSearch `json:"monitoredLogGroups"` // Array of log groups and search strings
}

// Handler is the Lambda function entry point
func handler(ctx context.Context, event Event) (string, error) {
	// Load AWS configuration with the correct region
	cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion("us-east-1"))
	if err != nil {
		log.Fatalf("Unable to load AWS SDK config: %v", err)
	}

	// Create CloudWatch Logs client
	cwlClient := cloudwatchlogs.NewFromConfig(cfg)
	cwClient := cloudwatch.NewFromConfig(cfg)

	var totalMatches int
	var triggeredAlarms int

	for _, logGroup := range event.MonitoredLogGroups {
		// Process each log group
		matches, err := processLogGroup(ctx, cwlClient, logGroup)
		if err != nil {
			log.Printf("Error processing log group %s: %v", logGroup.LogGroupName, err)
			continue
		}

		totalMatches += matches

		if matches > 0 {
			// Trigger or ensure a CloudWatch Alarm for this log group
			err := ensureCloudWatchAlarm(ctx, cwClient, logGroup.LogGroupName)
			if err != nil {
				log.Printf("Error ensuring alarm for log group %s: %v", logGroup.LogGroupName, err)
			} else {
				triggeredAlarms++
			}
		}
	}

	return fmt.Sprintf("Processed %d log groups, found %d total matches, triggered %d alarms",
		len(event.MonitoredLogGroups), totalMatches, triggeredAlarms), nil
}

// processLogGroup searches a log group for matching log events
func processLogGroup(ctx context.Context, cwlClient *cloudwatchlogs.Client, logGroup LogGroupSearch) (int, error) {
	// Calculate the time range (last 12 hours)
	endTime := time.Now().UnixMilli()
	startTime := endTime - (12 * 60 * 60 * 1000) // 12 hours in milliseconds

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
	return len(matchingEvents), nil
}

// ensureCloudWatchAlarm creates or updates an alarm for the log group
func ensureCloudWatchAlarm(ctx context.Context, cwClient *cloudwatch.Client, logGroupName string) error {
	alarmName := fmt.Sprintf("AlarmForLogGroup-%s", logGroupName)

	input := &cloudwatch.PutMetricAlarmInput{
		AlarmName:          aws.String(alarmName),
		ComparisonOperator: types.ComparisonOperatorGreaterThanOrEqualToThreshold,
		EvaluationPeriods:  aws.Int32(1),
		MetricName:         aws.String("ErrorCount"),
		Namespace:          aws.String("Custom/Errors"),
		Period:             aws.Int32(60),
		Statistic:          types.StatisticSum,
		Threshold:          aws.Float64(1),
		ActionsEnabled:     aws.Bool(true),
		AlarmActions:       []string{"arn:aws:sns:us-east-1:accnumber:topic-name"}, // Use correct SNS ARN with region
	}

	_, err := cwClient.PutMetricAlarm(ctx, input)
	if err != nil {
		return fmt.Errorf("failed to create/update alarm: %v", err)
	}

	log.Printf("CloudWatch Alarm ensured for log group %s", logGroupName)
	return nil
}

// Main function
func main() {
	lambda.Start(handler)
}
