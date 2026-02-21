package cron

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	cronv3 "github.com/robfig/cron/v3"

	"nextai/apps/gateway/internal/domain"
	"nextai/apps/gateway/internal/repo"
	"nextai/apps/gateway/internal/service/ports"
)

const (
	statusPaused    = "paused"
	statusResumed   = "resumed"
	statusRunning   = "running"
	statusSucceeded = "succeeded"
	statusFailed    = "failed"

	taskTypeText     = "text"
	taskTypeWorkflow = "workflow"

	workflowVersionV1 = "v1"
	workflowNodeStart = "start"
	workflowNodeText  = "text_event"
	workflowNodeDelay = "delay"
	workflowNodeIf    = "if_event"

	workflowNodeExecutionSkipped = "skipped"

	cronLeaseDirName = "cron-leases"
	qqChannelName    = "qq"
)

var ErrJobNotFound = errors.New("cron_job_not_found")
var ErrMaxConcurrencyReached = errors.New("cron_max_concurrency_reached")
var ErrDefaultProtected = errors.New("cron_default_protected")

var workflowIfConditionPattern = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*(==|!=)\s*(?:"([^"]*)"|'([^']*)'|(\S+))\s*$`)

var workflowIfAllowedFields = map[string]struct{}{
	"job_id":     {},
	"job_name":   {},
	"channel":    {},
	"user_id":    {},
	"session_id": {},
	"task_type":  {},
}

type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type channelError struct {
	Message string
	Err     error
}

func (e *channelError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return ""
}

func (e *channelError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type TaskExecutor func(ctx context.Context, job domain.CronJobSpec) (handled bool, err error)

type Dependencies struct {
	Store                   ports.StateStore
	DataDir                 string
	ChannelResolver         ports.ChannelResolver
	ExecuteConsoleAgentTask func(ctx context.Context, job domain.CronJobSpec, text string) error
	ExecuteTask             TaskExecutor
}

type Service struct {
	deps Dependencies
}

func NewService(deps Dependencies) *Service {
	return &Service{deps: deps}
}

func (s *Service) ListJobs() ([]domain.CronJobSpec, error) {
	if err := s.validateStore(); err != nil {
		return nil, err
	}

	out := make([]domain.CronJobSpec, 0)
	s.deps.Store.Read(func(state *repo.State) {
		for _, job := range state.CronJobs {
			out = append(out, job)
		}
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Service) CreateJob(job domain.CronJobSpec) (domain.CronJobSpec, error) {
	if err := s.validateStore(); err != nil {
		return domain.CronJobSpec{}, err
	}
	if code, err := validateJobSpec(&job); err != nil {
		return domain.CronJobSpec{}, &ValidationError{Code: code, Message: err.Error()}
	}

	now := time.Now().UTC()
	if err := s.deps.Store.Write(func(state *repo.State) error {
		state.CronJobs[job.ID] = job
		existing := state.CronStates[job.ID]
		state.CronStates[job.ID] = alignStateForMutation(job, normalizePausedState(existing), now)
		return nil
	}); err != nil {
		return domain.CronJobSpec{}, err
	}
	return job, nil
}

func (s *Service) GetJob(jobID string) (domain.CronJobView, error) {
	if err := s.validateStore(); err != nil {
		return domain.CronJobView{}, err
	}

	found := false
	var spec domain.CronJobSpec
	var state domain.CronJobState
	s.deps.Store.Read(func(st *repo.State) {
		spec, found = st.CronJobs[jobID]
		if found {
			state = st.CronStates[jobID]
		}
	})
	if !found {
		return domain.CronJobView{}, ErrJobNotFound
	}
	return domain.CronJobView{Spec: spec, State: state}, nil
}

func (s *Service) UpdateJob(jobID string, job domain.CronJobSpec) (domain.CronJobSpec, error) {
	if err := s.validateStore(); err != nil {
		return domain.CronJobSpec{}, err
	}
	if code, err := validateJobSpec(&job); err != nil {
		return domain.CronJobSpec{}, &ValidationError{Code: code, Message: err.Error()}
	}

	now := time.Now().UTC()
	if err := s.deps.Store.Write(func(st *repo.State) error {
		if _, ok := st.CronJobs[jobID]; !ok {
			return ErrJobNotFound
		}
		st.CronJobs[jobID] = job
		state := normalizePausedState(st.CronStates[jobID])
		st.CronStates[jobID] = alignStateForMutation(job, state, now)
		return nil
	}); err != nil {
		return domain.CronJobSpec{}, err
	}
	return job, nil
}

func (s *Service) DeleteJob(jobID string) (bool, error) {
	if err := s.validateStore(); err != nil {
		return false, err
	}

	jobID = strings.TrimSpace(jobID)
	deleted := false
	if err := s.deps.Store.Write(func(st *repo.State) error {
		if _, ok := st.CronJobs[jobID]; ok {
			if jobID == domain.DefaultCronJobID {
				return ErrDefaultProtected
			}
			delete(st.CronJobs, jobID)
			delete(st.CronStates, jobID)
			deleted = true
		}
		return nil
	}); err != nil {
		return false, err
	}
	return deleted, nil
}

func (s *Service) UpdateStatus(jobID, status string) error {
	if err := s.validateStore(); err != nil {
		return err
	}

	now := time.Now().UTC()
	return s.deps.Store.Write(func(st *repo.State) error {
		job, ok := st.CronJobs[jobID]
		if !ok {
			return ErrJobNotFound
		}
		state := normalizePausedState(st.CronStates[jobID])
		switch status {
		case statusPaused:
			state.Paused = true
			state.NextRunAt = nil
		case statusResumed:
			state.Paused = false
			state = alignStateForMutation(job, state, now)
		}
		state.LastStatus = &status
		st.CronStates[jobID] = state
		return nil
	})
}

func (s *Service) GetState(jobID string) (domain.CronJobState, error) {
	if err := s.validateStore(); err != nil {
		return domain.CronJobState{}, err
	}

	found := false
	var state domain.CronJobState
	s.deps.Store.Read(func(st *repo.State) {
		if _, ok := st.CronJobs[jobID]; ok {
			found = true
			state = st.CronStates[jobID]
		}
	})
	if !found {
		return domain.CronJobState{}, ErrJobNotFound
	}
	return state, nil
}

func (s *Service) SchedulerTick(now time.Time) ([]string, error) {
	if err := s.validateStore(); err != nil {
		return nil, err
	}

	stateUpdates := map[string]domain.CronJobState{}
	dueJobIDs := make([]string, 0)
	s.deps.Store.Read(func(st *repo.State) {
		for id, job := range st.CronJobs {
			current := st.CronStates[id]
			next := normalizePausedState(current)
			if !jobSchedulable(job, next) {
				next.NextRunAt = nil
				if !stateEqual(current, next) {
					stateUpdates[id] = next
				}
				continue
			}

			nextRunAt, dueAt, err := ResolveNextRunAt(job, next.NextRunAt, now)
			if err != nil {
				msg := err.Error()
				next.LastError = &msg
				next.NextRunAt = nil
				if !stateEqual(current, next) {
					stateUpdates[id] = next
				}
				continue
			}

			nextRun := nextRunAt.Format(time.RFC3339)
			next.NextRunAt = &nextRun
			next.LastError = nil
			if dueAt != nil && MisfireExceeded(dueAt, runtimeSpec(job), now) {
				failed := statusFailed
				msg := fmt.Sprintf("misfire skipped: scheduled_at=%s", dueAt.Format(time.RFC3339))
				next.LastStatus = &failed
				next.LastError = &msg
				dueAt = nil
			}
			if !stateEqual(current, next) {
				stateUpdates[id] = next
			}
			if dueAt != nil {
				dueJobIDs = append(dueJobIDs, id)
			}
		}
	})

	if len(stateUpdates) == 0 {
		return dueJobIDs, nil
	}
	if err := s.deps.Store.Write(func(st *repo.State) error {
		for id, next := range stateUpdates {
			if _, ok := st.CronJobs[id]; !ok {
				continue
			}
			st.CronStates[id] = next
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return dueJobIDs, nil
}

func (s *Service) ExecuteJob(jobID string) error {
	if err := s.validateStore(); err != nil {
		return err
	}

	var job domain.CronJobSpec
	found := false
	s.deps.Store.Read(func(st *repo.State) {
		job, found = st.CronJobs[jobID]
	})
	if !found {
		return ErrJobNotFound
	}

	runtime := runtimeSpec(job)
	slot, acquired, err := s.tryAcquireSlot(jobID, runtime)
	if err != nil {
		return err
	}
	if !acquired {
		if err := s.markExecutionSkipped(jobID, fmt.Sprintf("max_concurrency limit reached (%d)", runtime.MaxConcurrency)); err != nil {
			return err
		}
		return ErrMaxConcurrencyReached
	}
	defer s.releaseSlot(slot)

	startedAt := nowISO()
	running := statusRunning
	if err := s.deps.Store.Write(func(st *repo.State) error {
		target, ok := st.CronJobs[jobID]
		if !ok {
			return ErrJobNotFound
		}
		job = target
		state := normalizePausedState(st.CronStates[jobID])
		state.LastRunAt = &startedAt
		state.LastStatus = &running
		state.LastError = nil
		st.CronStates[jobID] = state
		return nil
	}); err != nil {
		return err
	}

	execCtx, cancel := context.WithTimeout(context.Background(), time.Duration(runtime.TimeoutSeconds)*time.Second)
	defer cancel()
	lastExecution, execErr := s.executeTask(execCtx, job)
	if errors.Is(execErr, context.DeadlineExceeded) {
		execErr = fmt.Errorf("cron execution timeout after %ds", runtime.TimeoutSeconds)
	}

	finalStatus := statusSucceeded
	var finalErr *string
	if execErr != nil {
		finalStatus = statusFailed
		msg := execErr.Error()
		finalErr = &msg
	}
	if err := s.deps.Store.Write(func(st *repo.State) error {
		if _, ok := st.CronJobs[jobID]; !ok {
			return nil
		}
		state := st.CronStates[jobID]
		state.LastStatus = &finalStatus
		state.LastError = finalErr
		state.LastExecution = lastExecution
		st.CronStates[jobID] = state
		return nil
	}); err != nil {
		return err
	}

	return execErr
}

func (s *Service) executeTask(ctx context.Context, job domain.CronJobSpec) (*domain.CronWorkflowExecution, error) {
	if s.deps.ExecuteTask != nil {
		handled, err := s.deps.ExecuteTask(ctx, job)
		if handled {
			return nil, err
		}
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	switch taskType(job) {
	case taskTypeText:
		text := strings.TrimSpace(job.Text)
		if text == "" {
			return nil, errors.New("cron text task requires non-empty text")
		}
		return nil, s.executeTextTask(ctx, job, text)
	case taskTypeWorkflow:
		execution, err := s.executeWorkflowTask(ctx, job)
		return execution, err
	default:
		return nil, fmt.Errorf("unsupported cron task_type=%q", job.TaskType)
	}
}

func (s *Service) executeTextTask(ctx context.Context, job domain.CronJobSpec, text string) error {
	channelName := strings.ToLower(resolveDispatchChannel(job))
	if channelName == qqChannelName {
		return errors.New("cron dispatch channel \"qq\" is inbound-only; use channel \"console\" to persist chat history")
	}
	if s.deps.ChannelResolver == nil {
		return errors.New("cron channel resolver is unavailable")
	}
	channelPlugin, channelCfg, resolvedChannelName, err := s.deps.ChannelResolver.ResolveChannel(channelName)
	if err != nil {
		return err
	}
	if resolvedChannelName == "console" {
		if s.deps.ExecuteConsoleAgentTask == nil {
			return errors.New("cron console agent executor is unavailable")
		}
		return s.deps.ExecuteConsoleAgentTask(ctx, job, text)
	}
	if err := channelPlugin.SendText(ctx, job.Dispatch.Target.UserID, job.Dispatch.Target.SessionID, text, channelCfg); err != nil {
		return &channelError{
			Message: fmt.Sprintf("failed to dispatch cron job to channel %q", resolvedChannelName),
			Err:     err,
		}
	}
	return nil
}

func (s *Service) executeWorkflowTask(ctx context.Context, job domain.CronJobSpec) (*domain.CronWorkflowExecution, error) {
	plan, err := buildWorkflowPlan(job.Workflow)
	if err != nil {
		return nil, fmt.Errorf("invalid cron workflow: %w", err)
	}

	startedAt := nowISO()
	execution := &domain.CronWorkflowExecution{
		RunID:       newRunID(),
		StartedAt:   startedAt,
		HadFailures: false,
		Nodes:       make([]domain.CronWorkflowNodeExecution, 0, len(plan.Order)),
	}

	var firstErr error
	for idx, node := range plan.Order {
		step := domain.CronWorkflowNodeExecution{
			NodeID:          node.ID,
			NodeType:        node.Type,
			ContinueOnError: node.ContinueOnError,
			StartedAt:       nowISO(),
		}

		runResult, runErr := s.executeWorkflowNode(ctx, job, node)
		finishedAt := nowISO()
		step.FinishedAt = &finishedAt
		if runErr != nil {
			step.Status = statusFailed
			errText := runErr.Error()
			step.Error = &errText
			execution.HadFailures = true
			if firstErr == nil {
				firstErr = fmt.Errorf("workflow node %s failed: %w", node.ID, runErr)
			}
		} else {
			step.Status = statusSucceeded
		}
		execution.Nodes = append(execution.Nodes, step)

		forceStop := runErr != nil && (errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded))
		shouldStop := runResult.Stop || (runErr != nil && (!node.ContinueOnError || forceStop))
		if !shouldStop {
			continue
		}
		for j := idx + 1; j < len(plan.Order); j++ {
			skippedNode := plan.Order[j]
			skippedAt := nowISO()
			skipped := domain.CronWorkflowNodeExecution{
				NodeID:          skippedNode.ID,
				NodeType:        skippedNode.Type,
				Status:          workflowNodeExecutionSkipped,
				ContinueOnError: skippedNode.ContinueOnError,
				StartedAt:       skippedAt,
				FinishedAt:      &skippedAt,
			}
			execution.Nodes = append(execution.Nodes, skipped)
		}
		break
	}

	finishedAt := nowISO()
	execution.FinishedAt = &finishedAt
	return execution, firstErr
}

type workflowNodeRunResult struct {
	Stop bool
}

func (s *Service) executeWorkflowNode(ctx context.Context, job domain.CronJobSpec, node domain.CronWorkflowNode) (workflowNodeRunResult, error) {
	switch node.Type {
	case workflowNodeText:
		text := strings.TrimSpace(node.Text)
		if text == "" {
			return workflowNodeRunResult{}, errors.New("workflow text_event requires non-empty text")
		}
		return workflowNodeRunResult{}, s.executeTextTask(ctx, job, text)
	case workflowNodeDelay:
		return workflowNodeRunResult{}, executeWorkflowDelay(ctx, node.DelaySeconds)
	case workflowNodeIf:
		matched, err := evaluateWorkflowIfCondition(node.IfCondition, job)
		if err != nil {
			return workflowNodeRunResult{}, err
		}
		if !matched {
			return workflowNodeRunResult{Stop: true}, nil
		}
		return workflowNodeRunResult{}, nil
	default:
		return workflowNodeRunResult{}, fmt.Errorf("unsupported workflow node type=%q", node.Type)
	}
}

func executeWorkflowDelay(ctx context.Context, seconds int) error {
	if seconds < 0 {
		return errors.New("workflow delay_seconds must be greater than or equal to 0")
	}
	if seconds == 0 {
		return nil
	}

	timer := time.NewTimer(time.Duration(seconds) * time.Second)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type workflowIfCondition struct {
	Field    string
	Operator string
	Value    string
}

func parseWorkflowIfCondition(raw string) (workflowIfCondition, error) {
	condition := strings.TrimSpace(raw)
	if condition == "" {
		return workflowIfCondition{}, errors.New("if_condition is required")
	}
	parts := workflowIfConditionPattern.FindStringSubmatch(condition)
	if len(parts) == 0 {
		return workflowIfCondition{}, errors.New("if_condition must match `<field> == <value>` or `<field> != <value>`")
	}
	field := strings.ToLower(strings.TrimSpace(parts[1]))
	if _, ok := workflowIfAllowedFields[field]; !ok {
		return workflowIfCondition{}, fmt.Errorf("if_condition field %q is unsupported", field)
	}
	value := parts[3]
	if value == "" {
		value = parts[4]
	}
	if value == "" {
		value = parts[5]
	}
	return workflowIfCondition{Field: field, Operator: parts[2], Value: value}, nil
}

func evaluateWorkflowIfCondition(raw string, job domain.CronJobSpec) (bool, error) {
	condition, err := parseWorkflowIfCondition(raw)
	if err != nil {
		return false, err
	}
	ctx := workflowIfContext(job)
	left, ok := ctx[condition.Field]
	if !ok {
		return false, fmt.Errorf("if_condition field %q is unsupported", condition.Field)
	}
	switch condition.Operator {
	case "==":
		return left == condition.Value, nil
	case "!=":
		return left != condition.Value, nil
	default:
		return false, fmt.Errorf("if_condition operator %q is unsupported", condition.Operator)
	}
}

func workflowIfContext(job domain.CronJobSpec) map[string]string {
	return map[string]string{
		"job_id":     strings.TrimSpace(job.ID),
		"job_name":   strings.TrimSpace(job.Name),
		"channel":    strings.ToLower(strings.TrimSpace(resolveDispatchChannel(job))),
		"user_id":    strings.TrimSpace(job.Dispatch.Target.UserID),
		"session_id": strings.TrimSpace(job.Dispatch.Target.SessionID),
		"task_type":  strings.ToLower(strings.TrimSpace(job.TaskType)),
	}
}

func BuildBizParams(job domain.CronJobSpec) map[string]interface{} {
	jobID := strings.TrimSpace(job.ID)
	jobName := strings.TrimSpace(job.Name)
	if jobID == "" && jobName == "" {
		return nil
	}
	cronPayload := map[string]interface{}{}
	if jobID != "" {
		cronPayload["job_id"] = jobID
	}
	if jobName != "" {
		cronPayload["job_name"] = jobName
	}
	return map[string]interface{}{"cron": cronPayload}
}

func validateJobSpec(job *domain.CronJobSpec) (string, error) {
	if job == nil {
		return "invalid_cron_task_type", errors.New("cron job is required")
	}
	job.ID = strings.TrimSpace(job.ID)
	job.Name = strings.TrimSpace(job.Name)
	if job.ID == "" || job.Name == "" {
		return "invalid_cron_task_type", errors.New("id and name are required")
	}

	switch taskType(*job) {
	case taskTypeText:
		text := strings.TrimSpace(job.Text)
		if text == "" {
			return "invalid_cron_task_type", errors.New("text is required for task_type=text")
		}
		job.TaskType = taskTypeText
		job.Text = text
		job.Workflow = nil
		return "", nil
	case taskTypeWorkflow:
		plan, err := buildWorkflowPlan(job.Workflow)
		if err != nil {
			return "invalid_cron_workflow", err
		}
		job.TaskType = taskTypeWorkflow
		job.Workflow = &plan.Workflow
		job.Text = ""
		return "", nil
	default:
		return "invalid_cron_task_type", fmt.Errorf("unsupported task_type=%q", strings.TrimSpace(job.TaskType))
	}
}

func taskType(job domain.CronJobSpec) string {
	t := strings.ToLower(strings.TrimSpace(job.TaskType))
	if t != "" {
		return t
	}
	if job.Workflow != nil {
		return taskTypeWorkflow
	}
	if strings.TrimSpace(job.Text) != "" {
		return taskTypeText
	}
	return t
}

type workflowPlan struct {
	Workflow domain.CronWorkflowSpec
	StartID  string
	NodeByID map[string]domain.CronWorkflowNode
	NextByID map[string]string
	Order    []domain.CronWorkflowNode
}

func buildWorkflowPlan(workflow *domain.CronWorkflowSpec) (*workflowPlan, error) {
	if workflow == nil {
		return nil, errors.New("workflow is required for task_type=workflow")
	}

	version := strings.ToLower(strings.TrimSpace(workflow.Version))
	if version != workflowVersionV1 {
		return nil, fmt.Errorf("unsupported workflow version=%q", workflow.Version)
	}
	if len(workflow.Nodes) < 2 {
		return nil, errors.New("workflow requires at least 2 nodes")
	}
	if len(workflow.Edges) < 1 {
		return nil, errors.New("workflow requires at least 1 edge")
	}

	nodeByID := make(map[string]domain.CronWorkflowNode, len(workflow.Nodes))
	normalizedNodes := make([]domain.CronWorkflowNode, 0, len(workflow.Nodes))
	startID := ""

	for _, rawNode := range workflow.Nodes {
		node := rawNode
		node.ID = strings.TrimSpace(node.ID)
		node.Type = strings.ToLower(strings.TrimSpace(node.Type))
		node.Title = strings.TrimSpace(node.Title)
		node.Text = strings.TrimSpace(node.Text)
		node.IfCondition = strings.TrimSpace(node.IfCondition)

		if node.ID == "" {
			return nil, errors.New("workflow node id is required")
		}
		if _, exists := nodeByID[node.ID]; exists {
			return nil, fmt.Errorf("workflow node id duplicated: %s", node.ID)
		}

		switch node.Type {
		case workflowNodeStart:
			node.ContinueOnError = false
			node.DelaySeconds = 0
			node.Text = ""
			node.IfCondition = ""
			if startID != "" {
				return nil, errors.New("workflow requires exactly one start node")
			}
			startID = node.ID
		case workflowNodeText:
			node.DelaySeconds = 0
			node.IfCondition = ""
			if node.Text == "" {
				return nil, fmt.Errorf("workflow node %s requires non-empty text", node.ID)
			}
		case workflowNodeDelay:
			node.Text = ""
			node.IfCondition = ""
			if node.DelaySeconds < 0 {
				return nil, fmt.Errorf("workflow node %s delay_seconds must be greater than or equal to 0", node.ID)
			}
		case workflowNodeIf:
			node.Text = ""
			node.DelaySeconds = 0
			if _, err := parseWorkflowIfCondition(node.IfCondition); err != nil {
				return nil, fmt.Errorf("workflow node %s if_condition invalid: %w", node.ID, err)
			}
		default:
			return nil, fmt.Errorf("workflow node %s has unsupported type=%q", node.ID, node.Type)
		}

		nodeByID[node.ID] = node
		normalizedNodes = append(normalizedNodes, node)
	}

	if startID == "" {
		return nil, errors.New("workflow requires exactly one start node")
	}

	edgeIDSet := map[string]struct{}{}
	nextByID := map[string]string{}
	inDegree := map[string]int{}
	outDegree := map[string]int{}
	normalizedEdges := make([]domain.CronWorkflowEdge, 0, len(workflow.Edges))

	for _, rawEdge := range workflow.Edges {
		edge := rawEdge
		edge.ID = strings.TrimSpace(edge.ID)
		edge.Source = strings.TrimSpace(edge.Source)
		edge.Target = strings.TrimSpace(edge.Target)

		if edge.ID == "" {
			return nil, errors.New("workflow edge id is required")
		}
		if _, exists := edgeIDSet[edge.ID]; exists {
			return nil, fmt.Errorf("workflow edge id duplicated: %s", edge.ID)
		}
		edgeIDSet[edge.ID] = struct{}{}

		if edge.Source == "" || edge.Target == "" {
			return nil, fmt.Errorf("workflow edge %s requires source and target", edge.ID)
		}
		if edge.Source == edge.Target {
			return nil, fmt.Errorf("workflow edge %s cannot link node to itself", edge.ID)
		}
		if _, ok := nodeByID[edge.Source]; !ok {
			return nil, fmt.Errorf("workflow edge %s source not found: %s", edge.ID, edge.Source)
		}
		if _, ok := nodeByID[edge.Target]; !ok {
			return nil, fmt.Errorf("workflow edge %s target not found: %s", edge.ID, edge.Target)
		}

		outDegree[edge.Source]++
		if outDegree[edge.Source] > 1 {
			return nil, fmt.Errorf("workflow node %s has more than one outgoing edge", edge.Source)
		}
		inDegree[edge.Target]++
		if inDegree[edge.Target] > 1 {
			return nil, fmt.Errorf("workflow node %s has more than one incoming edge", edge.Target)
		}
		nextByID[edge.Source] = edge.Target
		normalizedEdges = append(normalizedEdges, edge)
	}

	if inDegree[startID] > 0 {
		return nil, errors.New("workflow start node cannot have incoming edge")
	}
	if outDegree[startID] == 0 {
		return nil, errors.New("workflow start node must connect to at least one executable node")
	}

	reachable := map[string]bool{startID: true}
	order := make([]domain.CronWorkflowNode, 0, len(nodeByID)-1)
	cursor := startID
	for {
		nextID, ok := nextByID[cursor]
		if !ok {
			break
		}
		if reachable[nextID] {
			return nil, errors.New("workflow graph must be acyclic")
		}
		reachable[nextID] = true
		nextNode := nodeByID[nextID]
		if nextNode.Type == workflowNodeStart {
			return nil, errors.New("workflow start node cannot be targeted by execution path")
		}
		order = append(order, nextNode)
		cursor = nextID
	}

	if len(order) == 0 {
		return nil, errors.New("workflow requires at least one executable node")
	}
	for nodeID, node := range nodeByID {
		if node.Type == workflowNodeStart {
			continue
		}
		if !reachable[nodeID] {
			return nil, fmt.Errorf("workflow node %s is not reachable from start", nodeID)
		}
	}

	var viewport *domain.CronWorkflowViewport
	if workflow.Viewport != nil {
		v := *workflow.Viewport
		if v.Zoom <= 0 {
			v.Zoom = 1
		}
		viewport = &v
	}

	return &workflowPlan{
		Workflow: domain.CronWorkflowSpec{
			Version:  workflowVersionV1,
			Viewport: viewport,
			Nodes:    normalizedNodes,
			Edges:    normalizedEdges,
		},
		StartID:  startID,
		NodeByID: nodeByID,
		NextByID: nextByID,
		Order:    order,
	}, nil
}

func alignStateForMutation(job domain.CronJobSpec, state domain.CronJobState, now time.Time) domain.CronJobState {
	if !jobSchedulable(job, state) {
		state.NextRunAt = nil
		return state
	}
	nextRunAt, _, err := ResolveNextRunAt(job, nil, now)
	if err != nil {
		msg := err.Error()
		state.LastError = &msg
		state.NextRunAt = nil
		return state
	}

	nextRunAtText := nextRunAt.Format(time.RFC3339)
	state.NextRunAt = &nextRunAtText
	state.LastError = nil
	return state
}

func normalizePausedState(state domain.CronJobState) domain.CronJobState {
	if !state.Paused && state.LastStatus != nil && *state.LastStatus == statusPaused {
		state.Paused = true
	}
	return state
}

func stateEqual(a, b domain.CronJobState) bool {
	return stringPtrEqual(a.NextRunAt, b.NextRunAt) &&
		stringPtrEqual(a.LastRunAt, b.LastRunAt) &&
		stringPtrEqual(a.LastStatus, b.LastStatus) &&
		stringPtrEqual(a.LastError, b.LastError) &&
		a.Paused == b.Paused
}

func stringPtrEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func jobSchedulable(job domain.CronJobSpec, state domain.CronJobState) bool {
	return job.Enabled && !state.Paused
}

type leaseSlot struct {
	LeaseID string `json:"lease_id"`
	JobID   string `json:"job_id"`
	Owner   string `json:"owner"`
	Slot    int    `json:"slot"`

	AcquiredAt string `json:"acquired_at"`
	ExpiresAt  string `json:"expires_at"`
}

type leaseHandle struct {
	Path    string
	LeaseID string
}

func (s *Service) tryAcquireSlot(jobID string, runtime domain.CronRuntimeSpec) (*leaseHandle, bool, error) {
	maxConcurrency := runtime.MaxConcurrency
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}

	now := time.Now().UTC()
	ttl := time.Duration(runtime.TimeoutSeconds)*time.Second + 30*time.Second
	if ttl < 30*time.Second {
		ttl = 30 * time.Second
	}

	leaseID := newLeaseID()
	dir := filepath.Join(strings.TrimSpace(s.deps.DataDir), cronLeaseDirName, encodeJobID(jobID))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, false, err
	}

	for slot := 0; slot < maxConcurrency; slot++ {
		path := filepath.Join(dir, fmt.Sprintf("slot-%d.json", slot))
		if err := cleanupExpiredLease(path, now); err != nil {
			return nil, false, err
		}

		lease := leaseSlot{
			LeaseID:    leaseID,
			JobID:      jobID,
			Owner:      fmt.Sprintf("pid:%d", os.Getpid()),
			Slot:       slot,
			AcquiredAt: now.Format(time.RFC3339Nano),
			ExpiresAt:  now.Add(ttl).Format(time.RFC3339Nano),
		}
		body, err := json.Marshal(lease)
		if err != nil {
			return nil, false, err
		}

		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return nil, false, err
		}
		if _, err := f.Write(body); err != nil {
			_ = f.Close()
			_ = removeIfExists(path)
			return nil, false, err
		}
		if err := f.Close(); err != nil {
			_ = removeIfExists(path)
			return nil, false, err
		}
		return &leaseHandle{Path: path, LeaseID: leaseID}, true, nil
	}
	return nil, false, nil
}

func (s *Service) releaseSlot(slot *leaseHandle) {
	if slot == nil || strings.TrimSpace(slot.Path) == "" {
		return
	}

	body, err := os.ReadFile(slot.Path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		log.Printf("release cron lease read failed: path=%s err=%v", slot.Path, err)
		return
	}

	var lease leaseSlot
	if err := json.Unmarshal(body, &lease); err != nil {
		if rmErr := removeIfExists(slot.Path); rmErr != nil {
			log.Printf("release cron lease cleanup failed: path=%s err=%v", slot.Path, rmErr)
		}
		return
	}
	if lease.LeaseID != slot.LeaseID {
		return
	}
	if err := os.Remove(slot.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("release cron lease failed: path=%s err=%v", slot.Path, err)
	}
}

func cleanupExpiredLease(path string, now time.Time) error {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var lease leaseSlot
	if err := json.Unmarshal(body, &lease); err != nil {
		return removeIfExists(path)
	}

	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(lease.ExpiresAt))
	if err != nil {
		return removeIfExists(path)
	}
	if !now.After(expiresAt.UTC()) {
		return nil
	}
	return removeIfExists(path)
}

func removeIfExists(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func encodeJobID(jobID string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(jobID))
}

func newLeaseID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	return fmt.Sprintf("%d-%x", os.Getpid(), buf)
}

func newRunID() string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("run-%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	return fmt.Sprintf("run-%d-%x", os.Getpid(), buf)
}

func (s *Service) markExecutionSkipped(jobID, message string) error {
	failed := statusFailed
	return s.deps.Store.Write(func(st *repo.State) error {
		if _, ok := st.CronJobs[jobID]; !ok {
			return ErrJobNotFound
		}
		state := normalizePausedState(st.CronStates[jobID])
		state.LastStatus = &failed
		state.LastError = &message
		st.CronStates[jobID] = state
		return nil
	})
}

func runtimeSpec(job domain.CronJobSpec) domain.CronRuntimeSpec {
	out := job.Runtime
	if out.MaxConcurrency <= 0 {
		out.MaxConcurrency = 1
	}
	if out.TimeoutSeconds <= 0 {
		out.TimeoutSeconds = 30
	}
	if out.MisfireGraceSeconds < 0 {
		out.MisfireGraceSeconds = 0
	}
	return out
}

func scheduleType(job domain.CronJobSpec) string {
	t := strings.ToLower(strings.TrimSpace(job.Schedule.Type))
	if t == "" {
		return "interval"
	}
	return t
}

func interval(job domain.CronJobSpec) (time.Duration, error) {
	if scheduleType(job) != "interval" {
		return 0, fmt.Errorf("unsupported schedule.type=%q", job.Schedule.Type)
	}

	raw := strings.TrimSpace(job.Schedule.Cron)
	if raw == "" {
		return 0, errors.New("schedule.cron is required for interval jobs")
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		if secs <= 0 {
			return 0, errors.New("schedule interval must be greater than 0")
		}
		return time.Duration(secs) * time.Second, nil
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid schedule interval: %q", raw)
	}
	return parsed, nil
}

func ResolveNextRunAt(job domain.CronJobSpec, current *string, now time.Time) (time.Time, *time.Time, error) {
	switch scheduleType(job) {
	case "interval":
		iv, err := interval(job)
		if err != nil {
			return time.Time{}, nil, err
		}
		next, dueAt := resolveIntervalNextRunAt(current, iv, now)
		return next, dueAt, nil
	case "cron":
		schedule, loc, err := expression(job)
		if err != nil {
			return time.Time{}, nil, err
		}
		next, dueAt := resolveExpressionNextRunAt(current, schedule, loc, now)
		return next, dueAt, nil
	default:
		return time.Time{}, nil, fmt.Errorf("unsupported schedule.type=%q", job.Schedule.Type)
	}
}

func expression(job domain.CronJobSpec) (cronv3.Schedule, *time.Location, error) {
	raw := strings.TrimSpace(job.Schedule.Cron)
	if raw == "" {
		return nil, nil, errors.New("schedule.cron is required for cron jobs")
	}

	loc := time.UTC
	if tz := strings.TrimSpace(job.Schedule.Timezone); tz != "" {
		nextLoc, err := time.LoadLocation(tz)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid schedule.timezone=%q", job.Schedule.Timezone)
		}
		loc = nextLoc
	}

	parser := cronv3.NewParser(cronv3.SecondOptional | cronv3.Minute | cronv3.Hour | cronv3.Dom | cronv3.Month | cronv3.Dow | cronv3.Descriptor)
	schedule, err := parser.Parse(raw)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid cron expression: %w", err)
	}
	return schedule, loc, nil
}

func resolveIntervalNextRunAt(current *string, interval time.Duration, now time.Time) (time.Time, *time.Time) {
	next := now.Add(interval)
	if current == nil {
		return next, nil
	}

	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*current))
	if err != nil {
		return next, nil
	}
	if parsed.After(now) {
		return parsed, nil
	}

	dueAt := parsed
	for !parsed.After(now) {
		parsed = parsed.Add(interval)
	}
	return parsed, &dueAt
}

func resolveExpressionNextRunAt(current *string, schedule cronv3.Schedule, loc *time.Location, now time.Time) (time.Time, *time.Time) {
	nowInLoc := now.In(loc)
	next := schedule.Next(nowInLoc).UTC()
	if current == nil {
		return next, nil
	}

	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*current))
	if err != nil {
		return next, nil
	}
	if parsed.After(now) {
		return parsed, nil
	}

	dueAt := parsed
	cursor := parsed.In(loc)
	for i := 0; i < 2048 && !cursor.After(nowInLoc); i++ {
		nextCursor := schedule.Next(cursor)
		if !nextCursor.After(cursor) {
			return schedule.Next(nowInLoc).UTC(), &dueAt
		}
		cursor = nextCursor
	}
	if !cursor.After(nowInLoc) {
		cursor = schedule.Next(nowInLoc)
	}
	return cursor.UTC(), &dueAt
}

func MisfireExceeded(dueAt *time.Time, runtime domain.CronRuntimeSpec, now time.Time) bool {
	if dueAt == nil {
		return false
	}
	if runtime.MisfireGraceSeconds <= 0 {
		return false
	}
	grace := time.Duration(runtime.MisfireGraceSeconds) * time.Second
	return now.Sub(dueAt.UTC()) > grace
}

func resolveDispatchChannel(job domain.CronJobSpec) string {
	channelName := strings.TrimSpace(job.Dispatch.Channel)
	if channelName == "" {
		return "console"
	}
	return channelName
}

func (s *Service) validateStore() error {
	if s == nil || s.deps.Store == nil {
		return errors.New("cron service store is unavailable")
	}
	return nil
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339)
}
