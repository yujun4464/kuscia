// Copyright 2023 Ant Group Co., Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//nolint:dupl
package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/secretflow/kuscia/pkg/common"
	"github.com/secretflow/kuscia/pkg/crd/apis/kuscia/v1alpha1"
	kusciaclientset "github.com/secretflow/kuscia/pkg/crd/clientset/versioned"
	"github.com/secretflow/kuscia/pkg/kusciaapi/config"
	"github.com/secretflow/kuscia/pkg/kusciaapi/proxy"
	"github.com/secretflow/kuscia/pkg/kusciaapi/utils"
	"github.com/secretflow/kuscia/pkg/utils/nlog"
	"github.com/secretflow/kuscia/pkg/utils/resources"
	consts "github.com/secretflow/kuscia/pkg/web/constants"
	utils2 "github.com/secretflow/kuscia/pkg/web/utils"
	pberrorcode "github.com/secretflow/kuscia/proto/api/v1alpha1/errorcode"
	"github.com/secretflow/kuscia/proto/api/v1alpha1/kusciaapi"
)

type IJobService interface {
	CreateJob(ctx context.Context, request *kusciaapi.CreateJobRequest) *kusciaapi.CreateJobResponse
	QueryJob(ctx context.Context, request *kusciaapi.QueryJobRequest) *kusciaapi.QueryJobResponse
	BatchQueryJobStatus(ctx context.Context, request *kusciaapi.BatchQueryJobStatusRequest) *kusciaapi.BatchQueryJobStatusResponse
	StopJob(ctx context.Context, request *kusciaapi.StopJobRequest) *kusciaapi.StopJobResponse
	DeleteJob(ctx context.Context, request *kusciaapi.DeleteJobRequest) *kusciaapi.DeleteJobResponse
	WatchJob(ctx context.Context, request *kusciaapi.WatchJobRequest, event chan<- *kusciaapi.WatchJobEventResponse) error
	ApproveJob(ctx context.Context, request *kusciaapi.ApproveJobRequest) *kusciaapi.ApproveJobResponse
	SuspendJob(ctx context.Context, request *kusciaapi.SuspendJobRequest) *kusciaapi.SuspendJobResponse
	RestartJob(ctx context.Context, request *kusciaapi.RestartJobRequest) *kusciaapi.RestartJobResponse
	CancelJob(ctx context.Context, request *kusciaapi.CancelJobRequest) *kusciaapi.CancelJobResponse
}

type jobService struct {
	Initiator    string
	kusciaClient kusciaclientset.Interface
}

func NewJobService(config *config.KusciaAPIConfig) IJobService {
	switch config.RunMode {
	case common.RunModeLite:
		return &jobServiceLite{
			Initiator:       config.Initiator,
			kusciaAPIClient: proxy.NewKusciaAPIClient(""),
		}
	default:
		return &jobService{
			Initiator:    config.Initiator,
			kusciaClient: config.KusciaClient,
		}
	}
}

func (h *jobService) CreateJob(ctx context.Context, request *kusciaapi.CreateJobRequest) *kusciaapi.CreateJobResponse {
	// do validate
	if err := validateCreateJobRequest(request, h.Initiator); err != nil {
		return &kusciaapi.CreateJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, err.Error()),
		}
	}
	// auth handler
	if err := h.authHandlerJobCreate(ctx, request); err != nil {
		return &kusciaapi.CreateJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrAuthFailed, err.Error()),
		}
	}
	// convert createJobRequest to kuscia job
	tasks := request.Tasks
	kusciaTasks := make([]v1alpha1.KusciaTaskTemplate, len(tasks))
	for i, task := range tasks {
		// build kuscia task parties
		kusciaParties := make([]v1alpha1.Party, len(task.Parties))
		for j, party := range task.Parties {
			// build resources
			limitResource := corev1.ResourceList{}
			var resource *corev1.ResourceRequirements
			if party.Resources != nil {
				if party.Resources.Cpu != "" {
					if q, err := k8sresource.ParseQuantity(party.Resources.Cpu); err == nil {
						limitResource[corev1.ResourceCPU] = q
					} else {
						return &kusciaapi.CreateJobResponse{
							Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, fmt.Sprintf("parse input cpu resource failed: %v", err.Error())),
						}
					}
				}
				if party.Resources.Memory != "" {
					if q, err := k8sresource.ParseQuantity(party.Resources.Memory); err == nil {
						limitResource[corev1.ResourceMemory] = q
					} else {
						return &kusciaapi.CreateJobResponse{
							Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, fmt.Sprintf("parse input memory resource failed: %v", err.Error())),
						}
					}
				}
				resource = &corev1.ResourceRequirements{
					Limits: limitResource,
					// use limit as requests
					Requests: limitResource,
				}
			}
			var bandwidthLimits []v1alpha1.BandwidthLimit
			if len(party.BandwidthLimits) > 0 {
				for _, bw := range party.BandwidthLimits {
					if bw.LimitKbps <= 0 {
						return &kusciaapi.CreateJobResponse{
							Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, "bandwidth limit kbps can not be zero or negative"),
						}
					}
					if bw.DestinationId == "" {
						return &kusciaapi.CreateJobResponse{
							Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, "bandwidth limit destination id can not be empty"),
						}
					}
					bandwidthLimits = append(bandwidthLimits, v1alpha1.BandwidthLimit{
						DestinationID: bw.DestinationId,
						LimitKBps:     bw.LimitKbps,
					})
				}
			}

			kusciaParties[j] = v1alpha1.Party{
				DomainID:       party.DomainId,
				Role:           party.Role,
				Resources:      resource,
				BandwidthLimit: bandwidthLimits,
			}
		}
		// build kuscia task
		kusciaTask := v1alpha1.KusciaTaskTemplate{
			TaskID:          task.TaskId,
			Alias:           task.Alias,
			Dependencies:    task.Dependencies,
			AppImage:        task.AppImage,
			TaskInputConfig: task.TaskInputConfig,
			Parties:         kusciaParties,
			Priority:        int(task.Priority),
			Tolerable:       &task.Tolerable,
		}

		if task.ScheduleConfig != nil {
			kusciaTask.ScheduleConfig = buildScheduleConfigForKusciaTask(task.ScheduleConfig)
		}

		kusciaTasks[i] = kusciaTask
	}

	// custom_fields to labels
	labels := map[string]string{}
	for k, v := range request.CustomFields {
		labels[jobCustomField2Label(k)] = v
	}

	kusciaJob := &v1alpha1.KusciaJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:   request.JobId,
			Labels: labels,
		},
		Spec: v1alpha1.KusciaJobSpec{
			Initiator:      request.Initiator,
			MaxParallelism: utils.IntValue(request.MaxParallelism),
			ScheduleMode:   v1alpha1.KusciaJobScheduleModeBestEffort,
			Tasks:          kusciaTasks,
		},
	}

	// create kuscia job
	_, err := h.kusciaClient.KusciaV1alpha1().KusciaJobs(common.KusciaCrossDomain).Create(ctx, kusciaJob, metav1.CreateOptions{})
	if err != nil {
		return &kusciaapi.CreateJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrCreateJob, err.Error()),
		}
	}
	return &kusciaapi.CreateJobResponse{
		Status: utils2.BuildSuccessResponseStatus(),
		Data: &kusciaapi.CreateJobResponseData{
			JobId: request.JobId,
		},
	}
}

func buildScheduleConfigForKusciaTask(sc *kusciaapi.ScheduleConfig) *v1alpha1.ScheduleConfig {
	if sc == nil {
		return nil
	}

	if sc.TaskTimeoutSeconds <= 0 {
		sc.TaskTimeoutSeconds = 300
	}

	if sc.ResourceReservedSeconds <= 0 {
		sc.ResourceReservedSeconds = 30
	}

	if sc.ResourceReallocationIntervalSeconds <= 0 {
		sc.ResourceReallocationIntervalSeconds = 30
	}

	return &v1alpha1.ScheduleConfig{
		ResourceReservedSeconds: int(sc.ResourceReservedSeconds),
		LifecycleSeconds:        int(sc.TaskTimeoutSeconds),
		RetryIntervalSeconds:    int(sc.ResourceReallocationIntervalSeconds),
	}
}

func (h *jobService) QueryJob(ctx context.Context, request *kusciaapi.QueryJobRequest) *kusciaapi.QueryJobResponse {
	// do validate
	jobID := request.JobId
	if jobID == "" {
		return &kusciaapi.QueryJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, "job id can not be empty"),
		}
	}
	// build job status
	kusciaJob, jobStatus, err := h.buildJobStatusByID(ctx, jobID)
	if err != nil {
		return &kusciaapi.QueryJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrQueryJob, err.Error()),
		}
	}
	// custom fields
	prefixLen := len(common.JobCustomFieldsLabelPrefix)
	customFields := map[string]string{}
	for k, v := range kusciaJob.Labels {
		if strings.HasPrefix(k, common.JobCustomFieldsLabelPrefix) {
			customFields[k[prefixLen:]] = v
		}
	}
	// build task config
	kusciaJobSpec := kusciaJob.Spec
	kusciaTasks := kusciaJobSpec.Tasks
	taskConfigs := make([]*kusciaapi.TaskConfig, len(kusciaTasks))
	for i, task := range kusciaTasks {
		// build task parties
		taskParties := task.Parties
		parties := make([]*kusciaapi.Party, len(taskParties))
		for j, party := range taskParties {
			var bandwidthLimits []*kusciaapi.BandwidthLimit
			for _, bw := range party.BandwidthLimit {
				bandwidthLimits = append(bandwidthLimits, &kusciaapi.BandwidthLimit{
					DestinationId: bw.DestinationID,
					LimitKbps:     bw.LimitKBps,
				})
			}
			parties[j] = &kusciaapi.Party{
				DomainId:        party.DomainID,
				Role:            party.Role,
				BandwidthLimits: bandwidthLimits,
			}
		}

		// build task config
		taskConfig := &kusciaapi.TaskConfig{
			Alias:           task.Alias,
			TaskId:          task.TaskID,
			Parties:         parties,
			AppImage:        task.AppImage,
			Dependencies:    task.Dependencies,
			TaskInputConfig: task.TaskInputConfig,
			Priority:        int32(task.Priority),
		}

		if task.ScheduleConfig != nil {
			taskConfig.ScheduleConfig = buildScheduleConfigForKusciaAPI(task.ScheduleConfig)
		}

		taskConfigs[i] = taskConfig
	}

	// build job response
	jobResponse := &kusciaapi.QueryJobResponse{
		Status: utils2.BuildSuccessResponseStatus(),
		Data: &kusciaapi.QueryJobResponseData{
			JobId:          jobID,
			Initiator:      kusciaJobSpec.Initiator,
			MaxParallelism: utils.Int32Value(kusciaJobSpec.MaxParallelism),
			Tasks:          taskConfigs,
			Status:         jobStatus,
			CustomFields:   customFields,
		},
	}
	return jobResponse
}

func buildScheduleConfigForKusciaAPI(sc *v1alpha1.ScheduleConfig) *kusciaapi.ScheduleConfig {
	if sc == nil {
		return nil
	}

	if sc.LifecycleSeconds <= 0 {
		sc.LifecycleSeconds = 300
	}

	if sc.ResourceReservedSeconds <= 0 {
		sc.ResourceReservedSeconds = 30
	}

	if sc.RetryIntervalSeconds <= 0 {
		sc.RetryIntervalSeconds = 30
	}

	return &kusciaapi.ScheduleConfig{
		TaskTimeoutSeconds:                  int32(sc.LifecycleSeconds),
		ResourceReservedSeconds:             int32(sc.ResourceReservedSeconds),
		ResourceReallocationIntervalSeconds: int32(sc.RetryIntervalSeconds),
	}
}

func (h *jobService) DeleteJob(ctx context.Context, request *kusciaapi.DeleteJobRequest) *kusciaapi.DeleteJobResponse {
	// do validate
	jobID := request.JobId
	if jobID == "" {
		return &kusciaapi.DeleteJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, "job id can not be empty"),
		}
	}
	// auth handler
	if err := h.authHandlerJobDelete(ctx, jobID); err != nil {
		return &kusciaapi.DeleteJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrAuthFailed, err.Error()),
		}
	}
	// delete kuscia job
	err := h.kusciaClient.KusciaV1alpha1().KusciaJobs(common.KusciaCrossDomain).Delete(ctx, jobID, metav1.DeleteOptions{})
	if err != nil {
		return &kusciaapi.DeleteJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrDeleteJob, err.Error()),
		}
	}
	return &kusciaapi.DeleteJobResponse{
		Status: utils2.BuildSuccessResponseStatus(),
		Data: &kusciaapi.DeleteJobResponseData{
			JobId: jobID,
		},
	}
}

func (h *jobService) StopJob(ctx context.Context, request *kusciaapi.StopJobRequest) *kusciaapi.StopJobResponse {
	// do validate
	jobID := request.JobId
	if jobID == "" {
		return &kusciaapi.StopJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, "job id can not be empty"),
		}
	}
	// get domain from context
	_, domainID := GetRoleAndDomainFromCtx(ctx)
	if len(domainID) == 0 {
		return &kusciaapi.StopJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, "source domain header must be set"),
		}
	}

	job, err := h.kusciaClient.KusciaV1alpha1().KusciaJobs(common.KusciaCrossDomain).Get(ctx, jobID, metav1.GetOptions{})
	if err != nil {
		return &kusciaapi.StopJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrStopJob, err.Error()),
		}
	}

	// auth pre handler
	if err = h.authHandlerJobRetrieve(ctx, job); err != nil {
		return &kusciaapi.StopJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrAuthFailed, err.Error()),
		}
	}

	if job.Labels == nil || (job.Labels != nil && job.Labels[common.LabelJobStage] != string(v1alpha1.JobStopStage)) {
		// stop kuscia job
		h.setJobStage(job, v1alpha1.JobStopStage, h.Initiator)
		_, err = h.kusciaClient.KusciaV1alpha1().KusciaJobs(common.KusciaCrossDomain).Update(ctx, job, metav1.UpdateOptions{})
		if err != nil {
			return &kusciaapi.StopJobResponse{
				Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrStopJob, err.Error()),
			}
		}
	}
	return &kusciaapi.StopJobResponse{
		Status: utils2.BuildSuccessResponseStatus(),
		Data: &kusciaapi.StopJobResponseData{
			JobId: jobID,
		},
	}
}

func (h *jobService) ApproveJob(ctx context.Context, request *kusciaapi.ApproveJobRequest) *kusciaapi.ApproveJobResponse {
	// do validate
	jobID := request.JobId
	if jobID == "" {
		return &kusciaapi.ApproveJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, "job id can not be empty"),
		}
	}

	if request.Result != kusciaapi.ApproveResult_APPROVE_RESULT_ACCEPT &&
		request.Result != kusciaapi.ApproveResult_APPROVE_RESULT_REJECT {
		return &kusciaapi.ApproveJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, "request result must be legal"),
		}
	}

	// get domain from context
	role, domainID := GetRoleAndDomainFromCtx(ctx)
	var domainIDs []string
	if len(domainID) == 0 {
		return &kusciaapi.ApproveJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, "source domain header must be set"),
		}
	}
	job, err := h.kusciaClient.KusciaV1alpha1().KusciaJobs(common.KusciaCrossDomain).Get(ctx, jobID, metav1.GetOptions{})
	if err != nil {
		return &kusciaapi.ApproveJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrApproveJob, err.Error()),
		}
	}
	// auth handler
	if authErr := h.authHandlerJob(ctx, job); authErr != nil {
		return &kusciaapi.ApproveJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrAuthFailed, authErr.Error()),
		}
	}
	nlog.Infof("approve job %s result %v reason %s", jobID, request.Result, request.Reason)
	approveStatus := v1alpha1.JobRejected
	if request.Result == kusciaapi.ApproveResult_APPROVE_RESULT_ACCEPT {
		approveStatus = v1alpha1.JobAccepted
	}
	if role == consts.AuthRoleMaster {
		// get self cluster parties
		if selfParties, ok := job.Annotations[common.InterConnSelfPartyAnnotationKey]; ok {
			domainIDs = annotationToDomainList(selfParties)
		}
	} else { // role is domain
		// domain just approval itself party not all self cluster parties
		domainIDs = append(domainIDs, domainID)
	}
	for _, v := range domainIDs {
		if job.Status.ApproveStatus == nil {
			job.Status.ApproveStatus = make(map[string]v1alpha1.JobApprovePhase)
		}
		job.Status.ApproveStatus[v] = approveStatus
	}
	// update kuscia job approve status
	_, err = h.kusciaClient.KusciaV1alpha1().KusciaJobs(common.KusciaCrossDomain).UpdateStatus(ctx, job, metav1.UpdateOptions{})
	if err != nil {
		return &kusciaapi.ApproveJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrApproveJob, err.Error()),
		}
	}
	return &kusciaapi.ApproveJobResponse{
		Status: utils2.BuildSuccessResponseStatus(),
		Data: &kusciaapi.ApproveJobResponseData{
			JobId: jobID,
		},
	}
}

func (h *jobService) SuspendJob(ctx context.Context, request *kusciaapi.SuspendJobRequest) *kusciaapi.SuspendJobResponse {
	// do validate
	jobID := request.JobId
	if jobID == "" {
		return &kusciaapi.SuspendJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, "job id can not be empty"),
		}
	}
	// get domain from context
	_, domainID := GetRoleAndDomainFromCtx(ctx)
	if len(domainID) == 0 {
		return &kusciaapi.SuspendJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, "source domain header must be set"),
		}
	}
	job, err := h.kusciaClient.KusciaV1alpha1().KusciaJobs(common.KusciaCrossDomain).Get(ctx, jobID, metav1.GetOptions{})
	if err != nil {
		return &kusciaapi.SuspendJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrSuspendJob, err.Error()),
		}
	}
	// auth handler
	if authErr := h.authHandlerJob(ctx, job); authErr != nil {
		return &kusciaapi.SuspendJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrAuthFailed, authErr.Error()),
		}
	}
	if job.Status.Phase != v1alpha1.KusciaJobRunning {
		return &kusciaapi.SuspendJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrSuspendNotRunningJob, fmt.Sprintf("job: %s current status is %s can not be suspend.", job.Name, job.Status.Phase)),
		}
	}
	nlog.Infof("Suspend job: %s, reason: %s", jobID, request.Reason)
	// suspend kuscia job
	h.setJobStage(job, v1alpha1.JobSuspendStage, h.Initiator)

	_, err = h.kusciaClient.KusciaV1alpha1().KusciaJobs(common.KusciaCrossDomain).Update(ctx, job, metav1.UpdateOptions{})
	if err != nil {
		return &kusciaapi.SuspendJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrSuspendJob, err.Error()),
		}
	}
	return &kusciaapi.SuspendJobResponse{
		Status: utils2.BuildSuccessResponseStatus(),
		Data: &kusciaapi.SuspendJobResponseData{
			JobId: jobID,
		},
	}
}

func (h *jobService) RestartJob(ctx context.Context, request *kusciaapi.RestartJobRequest) *kusciaapi.RestartJobResponse {
	// do validate
	jobID := request.JobId
	if jobID == "" {
		return &kusciaapi.RestartJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, "job id can not be empty"),
		}
	}
	// get domain from context
	_, domainID := GetRoleAndDomainFromCtx(ctx)
	if len(domainID) == 0 {
		return &kusciaapi.RestartJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, "source domain header must be set"),
		}
	}
	job, err := h.kusciaClient.KusciaV1alpha1().KusciaJobs(common.KusciaCrossDomain).Get(ctx, jobID, metav1.GetOptions{})
	if err != nil {
		return &kusciaapi.RestartJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRestartJob, err.Error()),
		}
	}
	// auth handler
	if authErr := h.authHandlerJob(ctx, job); authErr != nil {
		return &kusciaapi.RestartJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrAuthFailed, authErr.Error()),
		}
	}
	if job.Status.Phase != v1alpha1.KusciaJobFailed && job.Status.Phase != v1alpha1.KusciaJobSuspended {
		return &kusciaapi.RestartJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRestartNotSuspendedOrFailedJob, fmt.Sprintf("job: %s current status is %s can not be restart.", job.Name, job.Status.Phase)),
		}
	}

	nlog.Infof("Restart job: %s, reason: %s", jobID, request.Reason)

	if job.Labels == nil || (job.Labels != nil && job.Labels[common.LabelJobStage] != string(v1alpha1.JobRestartStage)) {
		// restart kuscia job
		h.setJobStage(job, v1alpha1.JobRestartStage, h.Initiator)
		_, err = h.kusciaClient.KusciaV1alpha1().KusciaJobs(common.KusciaCrossDomain).Update(ctx, job, metav1.UpdateOptions{})
		if err != nil {
			return &kusciaapi.RestartJobResponse{
				Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrSuspendJob, err.Error()),
			}
		}
	}
	return &kusciaapi.RestartJobResponse{
		Status: utils2.BuildSuccessResponseStatus(),
		Data: &kusciaapi.RestartJobResponseData{
			JobId: jobID,
		},
	}
}

func (h *jobService) CancelJob(ctx context.Context, request *kusciaapi.CancelJobRequest) *kusciaapi.CancelJobResponse {
	// do validate
	jobID := request.JobId
	if jobID == "" {
		return &kusciaapi.CancelJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, "job id can not be empty"),
		}
	}
	// get domain from context
	_, domainID := GetRoleAndDomainFromCtx(ctx)
	if len(domainID) == 0 {
		return &kusciaapi.CancelJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, "source domain header must be set"),
		}
	}
	job, err := h.kusciaClient.KusciaV1alpha1().KusciaJobs(common.KusciaCrossDomain).Get(ctx, jobID, metav1.GetOptions{})
	if err != nil {
		return &kusciaapi.CancelJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrCancelJob, err.Error()),
		}
	}
	// auth handler
	if authErr := h.authHandlerJob(ctx, job); authErr != nil {
		return &kusciaapi.CancelJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrAuthFailed, authErr.Error()),
		}
	}
	nlog.Infof("Cancel job: %s, reason: %s", jobID, request.Reason)
	// cancel kuscia job
	h.setJobStage(job, v1alpha1.JobCancelStage, h.Initiator)
	_, err = h.kusciaClient.KusciaV1alpha1().KusciaJobs(common.KusciaCrossDomain).Update(ctx, job, metav1.UpdateOptions{})
	if err != nil {
		return &kusciaapi.CancelJobResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrCancelJob, err.Error()),
		}
	}
	return &kusciaapi.CancelJobResponse{
		Status: utils2.BuildSuccessResponseStatus(),
		Data: &kusciaapi.CancelJobResponseData{
			JobId: jobID,
		},
	}
}

func (h *jobService) setJobStage(job *v1alpha1.KusciaJob, stage v1alpha1.JobStage, domain string) {
	if job.Labels == nil {
		job.Labels = make(map[string]string)
	}
	job.Labels[common.LabelJobStage] = string(stage)
	job.Labels[common.LabelJobStageTrigger] = domain
	jobVersion := "1"
	if v, ok := job.Labels[common.LabelJobStageVersion]; ok {
		if iV, err := strconv.Atoi(v); err == nil {
			jobVersion = strconv.Itoa(iV + 1)
		}
	}
	job.Labels[common.LabelJobStageVersion] = jobVersion
}

func annotationToDomainList(value string) (domains []string) {
	domains = strings.Split(value, "_")
	return
}

func (h *jobService) BatchQueryJobStatus(ctx context.Context, request *kusciaapi.BatchQueryJobStatusRequest) *kusciaapi.BatchQueryJobStatusResponse {
	// do validate
	jobIDs := request.JobIds
	if err := validateBatchQueryJobStatusRequest(request); err != nil {
		return &kusciaapi.BatchQueryJobStatusResponse{
			Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrRequestValidate, err.Error()),
		}
	}
	// build job status
	jobStatuses := make([]*kusciaapi.JobStatus, len(jobIDs))
	for i, jobID := range jobIDs {
		_, jobStatusDetail, err := h.buildJobStatusByID(ctx, jobID)
		if err != nil {
			return &kusciaapi.BatchQueryJobStatusResponse{
				Status: utils2.BuildErrorResponseStatus(pberrorcode.ErrorCode_KusciaAPIErrQueryJobStatus, err.Error()),
			}
		}
		jobStatuses[i] = &kusciaapi.JobStatus{
			JobId:  jobID,
			Status: jobStatusDetail,
		}
	}

	return &kusciaapi.BatchQueryJobStatusResponse{
		Status: utils2.BuildSuccessResponseStatus(),
		Data: &kusciaapi.BatchQueryJobStatusResponseData{
			Jobs: jobStatuses,
		},
	}

}

func (h *jobService) WatchJob(ctx context.Context, request *kusciaapi.WatchJobRequest, eventCh chan<- *kusciaapi.WatchJobEventResponse) error {
	timeout := request.TimeoutSeconds
	if timeout < 0 || timeout > math.MaxInt32 {
		return fmt.Errorf("timeout seconds must be in [0,2^31-1]")
	}
	timeoutSeconds := int64(math.MaxInt32)
	if request.TimeoutSeconds > 0 {
		timeoutSeconds = request.TimeoutSeconds
	}
	// watch job status
	wJob, err := h.kusciaClient.KusciaV1alpha1().KusciaJobs(common.KusciaCrossDomain).Watch(ctx, metav1.ListOptions{
		TimeoutSeconds: &timeoutSeconds,
	})
	if err != nil {
		return err
	}
	expectedTypeJob := reflect.TypeOf(&v1alpha1.KusciaJob{})
	// watch task progress
	wTask, err := h.kusciaClient.KusciaV1alpha1().KusciaTasks(common.KusciaCrossDomain).Watch(ctx, metav1.ListOptions{
		TimeoutSeconds: &timeoutSeconds,
	})
	if err != nil {
		return err
	}
	expectedTypeTask := reflect.TypeOf(&v1alpha1.KusciaTask{})

	// Stopping the watcher should be idempotent and if we return from this function there's no way
	// we're coming back in with the same watch interface.
	defer wJob.Stop()
	defer wTask.Stop()

loop:
	for {
		select {
		case <-ctx.Done():
			return errors.New("stop requested")
		case event, ok := <-wJob.ResultChan():
			if !ok {
				break loop
			}
			if event.Type == watch.Error {
				return apierrors.FromObject(event.Object)
			}
			if expectedTypeJob != nil {
				if e, a := expectedTypeJob, reflect.TypeOf(event.Object); e != a {
					utilruntime.HandleError(fmt.Errorf("expected type %v, but watch event object had type %v", e, a))
					continue
				}
			}
			job, _ := event.Object.(*v1alpha1.KusciaJob)
			jobStatus, err := h.buildJobStatus(ctx, job)
			if !h.authHandlerJobWatch(ctx, job) {
				// No permission to watch
				role, domain := GetRoleAndDomainFromCtx(ctx)
				nlog.Debugf("No permission to watch, domain: %s role: %s, Job ID: %s", domain, role, job.Name)
				continue
			}
			if err != nil {
				return err
			}
			switch event.Type {
			case watch.Added:
				eventCh <- &kusciaapi.WatchJobEventResponse{
					Type:   kusciaapi.EventType_ADDED,
					Object: jobStatus,
				}
			case watch.Modified:
				eventCh <- &kusciaapi.WatchJobEventResponse{
					Type:   kusciaapi.EventType_MODIFIED,
					Object: jobStatus,
				}
			case watch.Deleted:
				eventCh <- &kusciaapi.WatchJobEventResponse{
					Type:   kusciaapi.EventType_DELETED,
					Object: jobStatus,
				}
			case watch.Bookmark:
				// A `Bookmark` means watch has synced here, just update the resourceVersion
			default:
				utilruntime.HandleError(fmt.Errorf("unable to understand watch event %#v", event))
			}
		case event, ok := <-wTask.ResultChan():
			if !ok {
				break loop
			}
			if event.Type == watch.Error {
				return apierrors.FromObject(event.Object)
			}
			switch event.Type {
			case watch.Modified: // only watch modified event
				if expectedTypeTask != nil {
					if e, a := expectedTypeTask, reflect.TypeOf(event.Object); e != a {
						utilruntime.HandleError(fmt.Errorf("expected type %v, but watch event object had type %v", e, a))
						continue
					}
				}
				task, _ := event.Object.(*v1alpha1.KusciaTask)
				// only update running task progress
				if task.Status.Phase == v1alpha1.TaskRunning && task.Status.Progress > 0 && task.Status.Progress < 1 {
					// get job via task owner ref
					ownerRef := metav1.GetControllerOf(task)
					if ownerRef == nil { // ignore task
						nlog.Infof("Task: %s does not have an owner.", task.Name)
						continue
					}
					job, err := h.kusciaClient.KusciaV1alpha1().KusciaJobs(common.KusciaCrossDomain).Get(ctx, ownerRef.Name, metav1.GetOptions{})
					if err != nil {
						nlog.Infof("Failed to get job: %s via task: %s owner ref.", ownerRef.Name, task.Name)
						continue
					}
					if !h.authHandlerJobWatch(ctx, job) {
						// No permission to watch
						role, domain := GetRoleAndDomainFromCtx(ctx)
						nlog.Debugf("No permission to watch, domain: %s role: %s, Job ID: %s", domain, role, job.Name)
						continue
					}
					// build job status and return response
					jobStatus, err := h.buildJobStatus(ctx, job)
					if err != nil {
						return err
					}
					eventCh <- &kusciaapi.WatchJobEventResponse{
						Type:   kusciaapi.EventType_MODIFIED,
						Object: jobStatus,
					}
				}
			default:
				// do noting
			}
		}
	}

	return nil
}

func (h *jobService) buildJobStatusByID(ctx context.Context, jobID string) (*v1alpha1.KusciaJob, *kusciaapi.JobStatusDetail, error) {
	kusciaJob, err := h.kusciaClient.KusciaV1alpha1().KusciaJobs(common.KusciaCrossDomain).Get(ctx, jobID, metav1.GetOptions{})
	if err != nil {
		return nil, nil, err
	}
	// auth pre handler
	if err = h.authHandlerJobRetrieve(ctx, kusciaJob); err != nil {
		return nil, nil, err
	}
	jobStatus, err := h.buildJobStatus(ctx, kusciaJob)
	if err != nil {
		return nil, nil, err
	}

	return kusciaJob, jobStatus.Status, nil
}

func (h *jobService) buildJobStatus(ctx context.Context, kusciaJob *v1alpha1.KusciaJob) (*kusciaapi.JobStatus, error) {
	if kusciaJob == nil {
		return nil, fmt.Errorf("kuscia job can not be nil")
	}
	kusciaJobStatus := kusciaJob.Status
	kusciaTasks := kusciaJob.Spec.Tasks
	// build job status
	statusDetail := &kusciaapi.JobStatusDetail{
		State:             getJobState(kusciaJobStatus.Phase),
		ErrMsg:            kusciaJobStatus.Message,
		CreateTime:        utils.TimeRfc3339String(&kusciaJob.CreationTimestamp),
		StartTime:         utils.TimeRfc3339String(kusciaJobStatus.StartTime),
		EndTime:           utils.TimeRfc3339String(kusciaJobStatus.CompletionTime),
		Tasks:             make([]*kusciaapi.TaskStatus, 0),
		StageStatusList:   make([]*kusciaapi.PartyStageStatus, 0),
		ApproveStatusList: make([]*kusciaapi.PartyApproveStatus, 0),
	}

	for k, v := range kusciaJobStatus.StageStatus {
		statusDetail.StageStatusList = append(statusDetail.StageStatusList, &kusciaapi.PartyStageStatus{
			DomainId: k,
			State:    string(v),
		})
	}

	for k, v := range kusciaJobStatus.ApproveStatus {
		statusDetail.ApproveStatusList = append(statusDetail.ApproveStatusList, &kusciaapi.PartyApproveStatus{
			DomainId: k,
			State:    string(v),
		})
	}

	// build task status
	for _, kt := range kusciaTasks {
		taskID := kt.TaskID
		ts := &kusciaapi.TaskStatus{
			TaskId: taskID,
			Alias:  kt.Alias,
			State:  getTaskState(v1alpha1.TaskPending),
		}
		if phase, ok := kusciaJobStatus.TaskStatus[taskID]; ok {
			ts.State = getTaskState(phase)
			task, err := h.kusciaClient.KusciaV1alpha1().KusciaTasks(common.KusciaCrossDomain).Get(ctx, taskID, metav1.GetOptions{})
			if err != nil {
				nlog.Warnf("Failed to get task [%s], %v", taskID, err.Error())
			} else {
				taskStatus := task.Status
				ts.ErrMsg = taskStatus.Message
				ts.CreateTime = utils.TimeRfc3339String(&task.CreationTimestamp)
				ts.StartTime = utils.TimeRfc3339String(taskStatus.StartTime)
				ts.EndTime = utils.TimeRfc3339String(taskStatus.CompletionTime)
				ts.Progress = taskStatus.Progress
				partyTaskStatus := make(map[string]v1alpha1.KusciaTaskPhase)
				for _, ps := range taskStatus.PartyTaskStatus {
					partyTaskStatus[ps.DomainID] = ps.Phase
				}

				partyErrMsg := make(map[string][]string)
				for _, podStatus := range taskStatus.PodStatuses {
					msg := ""
					if podStatus.Message != "" {
						msg = fmt.Sprintf("%v;", podStatus.Message)
					}
					if podStatus.TerminationLog != "" {
						msg += podStatus.TerminationLog
					}
					partyErrMsg[podStatus.Namespace] = append(partyErrMsg[podStatus.Namespace], msg)
				}

				partyEndpoints := make(map[string][]*kusciaapi.JobPartyEndpoint)
				for _, svcStatus := range taskStatus.ServiceStatuses {
					ep := fmt.Sprintf("%v.%v.svc", svcStatus.ServiceName, svcStatus.Namespace)
					if svcStatus.Scope == v1alpha1.ScopeDomain {
						ep = fmt.Sprintf("%v:%v", ep, svcStatus.PortNumber)
					}
					partyEndpoints[svcStatus.Namespace] = append(partyEndpoints[svcStatus.Namespace], &kusciaapi.JobPartyEndpoint{
						PortName: svcStatus.PortName,
						Scope:    string(svcStatus.Scope),
						Endpoint: ep,
					})
				}

				ts.Parties = make([]*kusciaapi.PartyStatus, 0)
				for partyID := range partyErrMsg {
					ts.Parties = append(ts.Parties, &kusciaapi.PartyStatus{
						DomainId:  partyID,
						State:     getTaskState(partyTaskStatus[partyID]),
						ErrMsg:    strings.Join(partyErrMsg[partyID], ","),
						Endpoints: partyEndpoints[partyID],
					})
				}
			}
		}

		statusDetail.Tasks = append(statusDetail.Tasks, ts)
	}
	statusDetail.ErrMsg = h.aggregateErrorMessage(kusciaJobStatus.Message, kusciaJobStatus.Phase, statusDetail.Tasks)
	return &kusciaapi.JobStatus{
		JobId:  kusciaJob.Name,
		Status: statusDetail,
	}, nil
}

func (h *jobService) authHandlerJobCreate(ctx context.Context, request *kusciaapi.CreateJobRequest) error {
	role, domainID := GetRoleAndDomainFromCtx(ctx)
	if domainID == request.Initiator {
		return nil
	}
	if role == consts.AuthRoleDomain {
		for _, task := range request.Tasks {
			for _, p := range task.Parties {
				if p.GetDomainId() == domainID {
					return nil
				}
			}
		}
		return fmt.Errorf("domain's KusciaAPI could only create the job that the domain as a participant in the job")
	}
	return nil
}

func (h *jobService) authHandlerJobDelete(ctx context.Context, jobID string) error {
	role, domainID := GetRoleAndDomainFromCtx(ctx)
	if role == consts.AuthRoleDomain {
		kusciaJob, err := h.kusciaClient.KusciaV1alpha1().KusciaJobs(common.KusciaCrossDomain).Get(ctx, jobID, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if domainID == kusciaJob.Spec.Initiator {
			return nil
		}
		for _, task := range kusciaJob.Spec.Tasks {
			for _, p := range task.Parties {
				if p.DomainID == domainID {
					return nil
				}
			}
		}
		return fmt.Errorf("domain's KusciaAPI could only delete the job that the domain as a participant in the job")
	}
	return nil
}

func (h *jobService) authHandlerJobRetrieve(ctx context.Context, kusciaJob *v1alpha1.KusciaJob) error {
	role, domainID := GetRoleAndDomainFromCtx(ctx)
	if domainID == kusciaJob.Spec.Initiator {
		return nil
	}
	if role == consts.AuthRoleDomain {
		for _, task := range kusciaJob.Spec.Tasks {
			for _, p := range task.Parties {
				if p.DomainID == domainID {
					return nil
				}
			}
		}
		return fmt.Errorf("domain's KusciaAPI could only retrieve the job that the domain as a participant in the job")
	}
	return nil
}

func (h *jobService) authHandlerJobWatch(ctx context.Context, kusciaJob *v1alpha1.KusciaJob) bool {
	role, domainID := GetRoleAndDomainFromCtx(ctx)
	if domainID == kusciaJob.Spec.Initiator {
		return true
	}
	if role == consts.AuthRoleDomain {
		for _, task := range kusciaJob.Spec.Tasks {
			for _, p := range task.Parties {
				if p.DomainID == domainID {
					return true
				}
			}
		}
		return false
	}
	return true
}

func (h *jobService) authHandlerJob(ctx context.Context, kusciaJob *v1alpha1.KusciaJob) error {
	role, domainID := GetRoleAndDomainFromCtx(ctx)
	if domainID == kusciaJob.Spec.Initiator {
		return nil
	}
	if role == consts.AuthRoleDomain {
		for _, task := range kusciaJob.Spec.Tasks {
			for _, p := range task.Parties {
				if p.DomainID == domainID {
					return nil
				}
			}
		}
		return fmt.Errorf("domain's KusciaAPI could only approve the job that the domain as a participant in the job")
	}
	return nil
}

func validateCreateJobRequest(request *kusciaapi.CreateJobRequest, domainID string) error {
	// jobID can not be empty
	jobID := request.JobId
	if jobID == "" {
		return fmt.Errorf("job id can not be empty")
	}
	// do k8s validate
	if err := resources.ValidateK8sName(request.JobId, "job_id"); err != nil {
		return err
	}
	// tasks can not be empty
	if len(request.Tasks) == 0 {
		return fmt.Errorf("tasks can not be empty")
	}
	// check initiator
	if err := validateInitiator(domainID, request.Initiator, request.Tasks); err != nil {
		return err
	}
	// check maxParallelism
	maxParallelism := request.MaxParallelism
	if maxParallelism <= 0 {
		request.MaxParallelism = 1
	}
	// taskId, parties can not be empty
	tasks := request.Tasks
	for i, task := range tasks {
		if task.Alias == "" {
			return fmt.Errorf("task alias can not be empty on tasks[%d]", i)
		}
		parties := task.Parties
		if len(parties) == 0 {
			return fmt.Errorf("parties can not be empty on tasks[%d]", i)
		}
		for _, party := range parties {
			if party.DomainId == "" {
				return fmt.Errorf("party domain id can not be empty")
			}
		}
	}
	return nil
}

func validateInitiator(domainID, initiator string, tasks []*kusciaapi.Task) error {
	if initiator == "" {
		return fmt.Errorf("initiator can not be empty")
	}
	if domainID != "" && domainID != initiator {
		return fmt.Errorf("initiator is %s, but initiator must be %s in P2P", initiator, domainID)
	}
	return nil
}

func validateBatchQueryJobStatusRequest(request *kusciaapi.BatchQueryJobStatusRequest) error {
	if len(request.JobIds) == 0 {
		return fmt.Errorf("job ids can not be empty")
	}
	for _, jobID := range request.JobIds {
		if jobID == "" {
			return fmt.Errorf("job id can not be empty")
		}
	}
	return nil
}

func getJobState(jobPhase v1alpha1.KusciaJobPhase) string {
	switch jobPhase {
	case "", v1alpha1.KusciaJobPending:
		return kusciaapi.JobState_Pending.String()
	case v1alpha1.KusciaJobRunning:
		return kusciaapi.JobState_Running.String()
	case v1alpha1.KusciaJobFailed:
		return kusciaapi.JobState_Failed.String()
	case v1alpha1.KusciaJobSucceeded:
		return kusciaapi.JobState_Succeeded.String()
	case v1alpha1.KusciaJobAwaitingApproval:
		return kusciaapi.JobState_AwaitingApproval.String()
	case v1alpha1.KusciaJobApprovalReject:
		return kusciaapi.JobState_ApprovalReject.String()
	case v1alpha1.KusciaJobCancelled:
		return kusciaapi.JobState_Cancelled.String()
	case v1alpha1.KusciaJobSuspended:
		return kusciaapi.JobState_Suspended.String()
	default:
		return kusciaapi.JobState_Unknown.String()
	}
}

func getTaskState(taskPhase v1alpha1.KusciaTaskPhase) string {
	switch taskPhase {
	case "", v1alpha1.TaskPending:
		return kusciaapi.JobState_Pending.String()
	case v1alpha1.TaskRunning:
		return kusciaapi.JobState_Running.String()
	case v1alpha1.TaskFailed:
		return kusciaapi.JobState_Failed.String()
	case v1alpha1.TaskSucceeded:
		return kusciaapi.JobState_Succeeded.String()
	default:
		return kusciaapi.JobState_Unknown.String()
	}
}

func jobCustomField2Label(name string) string {
	return fmt.Sprintf("%s%s", common.JobCustomFieldsLabelPrefix, name)
}

func (h *jobService) aggregateErrorMessage(jobMsg string, jobPhase v1alpha1.KusciaJobPhase, tasks []*kusciaapi.TaskStatus) string {
	// if jobMsg is not empty, return it
	if jobMsg != "" {
		return jobMsg
	}

	if jobPhase != v1alpha1.KusciaJobFailed {
		return jobMsg
	}

	var errMsgs []string
	for _, task := range tasks {
		if task.State == kusciaapi.JobState_Failed.String() {
			for _, party := range task.Parties {
				if party.State == kusciaapi.JobState_Failed.String() && party.ErrMsg != "" {
					errMsgs = append(errMsgs, fmt.Sprintf("Task[%s]-Party[%s]: %s",
						task.Alias, party.DomainId, party.ErrMsg))
				}
			}
		}
	}

	if len(errMsgs) > 0 {
		return strings.Join(errMsgs, "; ")
	}

	return jobMsg
}
