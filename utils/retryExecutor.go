package utils

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ErrorState is used to control error back-offs
type ErrorState struct {
	// Stage is an identifier for the stage of reconciliation that the error occurred in
	Stage string `json:"stage"`
	// LastBackOffPeriodInSeconds is the duration (in seconds) used for the last back-off
	LastBackOffPeriodInSeconds int `json:"lastBackOffPeriodInSeconds"`
	// SpecGeneration is the Generation from the Spec for the last error
	SpecGeneration int64 `json:"specGeneration"`
}

// ObjectWithErrorState represents a runtime.Object with the ability to store error state
type ObjectWithErrorState interface {
	runtime.Object
	GetGeneration() int64 // from metav1.ObjectMeta
	DeepCopyObjectWithErrorState() ObjectWithErrorState
	GetErrorState() *ErrorState
	SetErrorState(errorState *ErrorState)
}

// RetryExecutor performs function execution with back-off retry
type RetryExecutor struct {
	eventRecorder record.EventRecorder
	log           logr.Logger
	statusClient  client.StatusClient
}

// NewRetryExecutor returns a new initialized RetryExecutor
func NewRetryExecutor(eventRecorder record.EventRecorder, log logr.Logger, statusClient client.StatusClient) RetryExecutor {
	return RetryExecutor{
		eventRecorder: eventRecorder,
		log:           log,
		statusClient:  statusClient,
	}
}

// ExecuteWithRetry executes the specified action with back-off on errors
func (r *RetryExecutor) ExecuteWithRetry(ctx context.Context, objectWithErrorState ObjectWithErrorState, stage string, eventReason string, action func() error) (*ctrl.Result, error) {

	err := action()

	log := r.log.WithValues("stage", stage)

	if err == nil {
		// Success
		log.Info("Successfully executed")
		r.eventRecorder.Event(objectWithErrorState, corev1.EventTypeWarning, eventReason, "Success")

		if err = r.clearBackoffDuration(ctx, objectWithErrorState); err != nil {
			return &ctrl.Result{}, err
		}
	} else {
		log.Info(fmt.Sprintf("Failed to execute: %s", err))
		r.eventRecorder.Event(objectWithErrorState, corev1.EventTypeWarning, eventReason, err.Error())
		backOff, err := r.getBackoffDuration(ctx, objectWithErrorState, stage) // This updates the retry interval saved in the status
		if err != nil {
			return &ctrl.Result{}, err
		}
		if backOff < 0 {
			// Reached maximum number of attempts
			log.Info("Reached retry limit")
			r.eventRecorder.Event(objectWithErrorState, corev1.EventTypeWarning, eventReason+"-Final", "Reached retry limit")
			return &ctrl.Result{}, nil
		}
		log.Info("Requeuing to retry", "backoff", backOff)
		return &ctrl.Result{RequeueAfter: backOff}, nil
	}
	return nil, nil
}

// getBackoffDuration determines the back-off duration to use for the specified stage updating the status with the new values.
// If error is nil then duration is -ve for maximum retries reached or +ve for the duration to use
func (r *RetryExecutor) getBackoffDuration(ctx context.Context, objectWithErrorState ObjectWithErrorState, stage string) (time.Duration, error) {
	const MaximumRetryDuration = 512
	newObject := objectWithErrorState.DeepCopyObjectWithErrorState()
	errorState := objectWithErrorState.GetErrorState()
	newErrorState := newObject.GetErrorState()
	if errorState != nil &&
		errorState.Stage == stage &&
		errorState.SpecGeneration == objectWithErrorState.GetGeneration() {
		// Have a previous error state for the matching stage and generation
		if errorState.LastBackOffPeriodInSeconds > MaximumRetryDuration {
			return -1 * time.Second, nil
		}
		newErrorState.LastBackOffPeriodInSeconds = errorState.LastBackOffPeriodInSeconds * 2
	} else {
		newErrorState = &ErrorState{
			Stage:                      stage,
			LastBackOffPeriodInSeconds: 1,
		}
	}
	newErrorState.SpecGeneration = objectWithErrorState.GetGeneration()
	newObject.SetErrorState(newErrorState)

	if err := r.statusClient.Status().Patch(ctx, newObject, client.MergeFrom(objectWithErrorState)); err != nil {
		return 0, err
	}

	duration := time.Duration(newErrorState.LastBackOffPeriodInSeconds) * time.Second
	return duration, nil
}
func (r *RetryExecutor) clearBackoffDuration(ctx context.Context, objectWithErrorState ObjectWithErrorState) error {
	if objectWithErrorState.GetErrorState() != nil {
		newObject := objectWithErrorState.DeepCopyObjectWithErrorState()
		newObject.SetErrorState(nil)
		if err := r.statusClient.Status().Patch(ctx, newObject, client.MergeFrom(objectWithErrorState)); err != nil {
			return err
		}
	}
	return nil
}
