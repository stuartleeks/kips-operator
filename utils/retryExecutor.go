package utils

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kipsv1alpha1 "faux.ninja/kips-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

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
func (r *RetryExecutor) ExecuteWithRetry(ctx context.Context, serviceBridge *kipsv1alpha1.ServiceBridge, stage string, eventReason string, action func() error) (*ctrl.Result, error) {

	err := action()

	log := r.log.WithValues("stage", stage)

	if err == nil {
		// Success
		log.Info("Successfully executed")
		r.eventRecorder.Event(serviceBridge, corev1.EventTypeWarning, eventReason, "Success")

		if err = r.clearBackoffDuration(ctx, serviceBridge); err != nil {
			return &ctrl.Result{}, err
		}
	} else {
		log.Info(fmt.Sprintf("Failed to execute: %s", err))
		r.eventRecorder.Event(serviceBridge, corev1.EventTypeWarning, eventReason, err.Error())
		backOff, err := r.getBackoffDuration(ctx, serviceBridge, stage) // This updates the retry interval saved in the status
		if err != nil {
			return &ctrl.Result{}, err
		}
		if backOff < 0 {
			// Reached maximum number of attempts
			log.Info("Reached retry limit")
			r.eventRecorder.Event(serviceBridge, corev1.EventTypeWarning, eventReason+"-Final", "Reached retry limit")
			return &ctrl.Result{}, nil
		}
		log.Info("Requeuing to retry", "backoff", backOff)
		return &ctrl.Result{RequeueAfter: backOff}, nil
	}
	return nil, nil
}

// getBackoffDuration determines the back-off duration to use for the specified stage updating the status with the new values.
// If error is nil then duration is -ve for maximum retries reached or +ve for the duration to use
func (r *RetryExecutor) getBackoffDuration(ctx context.Context, serviceBridge *kipsv1alpha1.ServiceBridge, stage string) (time.Duration, error) {
	const MaximumRetryDuration = 512
	newBridge := serviceBridge.DeepCopy()
	if serviceBridge.Status.ErrorState != nil &&
		serviceBridge.Status.ErrorState.Stage == stage &&
		serviceBridge.Status.ErrorState.SpecGeneration == serviceBridge.Generation {
		// Have a previous error state for the matching stage and generation
		if newBridge.Status.ErrorState.LastBackOffPeriodInSeconds > MaximumRetryDuration {
			return -1 * time.Second, nil
		}
		newBridge.Status.ErrorState.LastBackOffPeriodInSeconds = newBridge.Status.ErrorState.LastBackOffPeriodInSeconds * 2
	} else {
		newBridge.Status.ErrorState = &kipsv1alpha1.ErrorState{
			Stage:                      stage,
			LastBackOffPeriodInSeconds: 1,
		}
	}
	newBridge.Status.ErrorState.SpecGeneration = serviceBridge.Generation

	if err := r.statusClient.Status().Patch(ctx, newBridge, client.MergeFrom(serviceBridge)); err != nil {
		return 0, err
	}

	duration := time.Duration(newBridge.Status.ErrorState.LastBackOffPeriodInSeconds) * time.Second
	return duration, nil
}
func (r *RetryExecutor) clearBackoffDuration(ctx context.Context, serviceBridge *kipsv1alpha1.ServiceBridge) error {

	if serviceBridge.Status.ErrorState != nil {
		newBridge := serviceBridge.DeepCopy()
		newBridge.Status.ErrorState = nil
		if err := r.statusClient.Status().Patch(ctx, newBridge, client.MergeFrom(serviceBridge)); err != nil {
			return err
		}
	}
	return nil
}
