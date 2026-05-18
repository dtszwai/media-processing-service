package worker

import (
	"context"
	"testing"

	lambdaevents "github.com/aws/aws-lambda-go/events"
)

func TestLambdaHandlerUnwrapsSNSEnvelope(t *testing.T) {
	var got []byte
	h := lambdaHandler(Config{
		Service: "test-worker",
		Handler: func(_ context.Context, body []byte) error {
			got = append([]byte(nil), body...)
			return nil
		},
	})

	resp, err := h(context.Background(), lambdaevents.SQSEvent{
		Records: []lambdaevents.SQSMessage{{
			MessageId: "msg-1",
			Body:      `{"Type":"Notification","Message":"{\"event_type\":\"media.v1.created\"}"}`,
		}},
	})
	if err != nil {
		t.Fatalf("lambda handler returned error: %v", err)
	}
	if len(resp.BatchItemFailures) != 0 {
		t.Fatalf("batch failures = %#v", resp.BatchItemFailures)
	}
	if string(got) != `{"event_type":"media.v1.created"}` {
		t.Fatalf("handler body = %s", got)
	}
}

func TestLambdaHandlerPassesThroughRawSQSBody(t *testing.T) {
	var got []byte
	h := lambdaHandler(Config{
		Service: "test-worker",
		Handler: func(_ context.Context, body []byte) error {
			got = append([]byte(nil), body...)
			return nil
		},
	})

	_, err := h(context.Background(), lambdaevents.SQSEvent{
		Records: []lambdaevents.SQSMessage{{
			MessageId: "msg-1",
			Body:      `{"Records":[{"eventName":"ObjectCreated:Put"}]}`,
		}},
	})
	if err != nil {
		t.Fatalf("lambda handler returned error: %v", err)
	}
	if string(got) != `{"Records":[{"eventName":"ObjectCreated:Put"}]}` {
		t.Fatalf("handler body = %s", got)
	}
}
