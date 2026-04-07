package controller

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"pentagi/pkg/config"
	"pentagi/pkg/database"
	"pentagi/pkg/docker"
	"pentagi/pkg/graph/subscriptions"
	"pentagi/pkg/providers"
	"pentagi/pkg/providers/provider"
	"pentagi/pkg/tools"

	"github.com/sirupsen/logrus"
)

var (
	ErrFlowNotFound       = fmt.Errorf("flow not found")
	ErrFlowAlreadyStopped = fmt.Errorf("flow already stopped")
)

type FlowController interface {
	CreateFlow(
		ctx context.Context,
		userID int64,
		input string,
		prvname provider.ProviderName,
		prvtype provider.ProviderType,
		functions *tools.Functions,
	) (FlowWorker, error)
	CreateAssistant(
		ctx context.Context,
		userID int64,
		flowID int64,
		input string,
		useAgents bool,
		prvname provider.ProviderName,
		prvtype provider.ProviderType,
		functions *tools.Functions,
	) (AssistantWorker, error)
	LoadFlows(ctx context.Context) error
	ListFlows(ctx context.Context) []FlowWorker
	GetFlow(ctx context.Context, flowID int64) (FlowWorker, error)
	StopFlow(ctx context.Context, flowID int64) error
	FinishFlow(ctx context.Context, flowID int64) error
	RenameFlow(ctx context.Context, flowID int64, title string) error
}

type flowController struct {
	db     database.Querier
	mx     *sync.Mutex
	cfg    *config.Config
	flows  map[int64]FlowWorker
	docker docker.DockerClient
	provs  providers.ProviderController
	subs   subscriptions.SubscriptionsController
	alc    AgentLogController
	mlc    MsgLogController
	aslc   AssistantLogController
	slc    SearchLogController
	tlc    TermLogController
	vslc   VectorStoreLogController
	sc     ScreenshotController
}

func NewFlowController(
	db database.Querier,
	cfg *config.Config,
	docker docker.DockerClient,
	provs providers.ProviderController,
	subs subscriptions.SubscriptionsController,
) FlowController {
	return &flowController{
		db:     db,
		mx:     &sync.Mutex{},
		cfg:    cfg,
		flows:  make(map[int64]FlowWorker),
		docker: docker,
		provs:  provs,
		subs:   subs,
		alc:    NewAgentLogController(db),
		mlc:    NewMsgLogController(db),
		aslc:   NewAssistantLogController(db),
		slc:    NewSearchLogController(db),
		tlc:    NewTermLogController(db),
		vslc:   NewVectorStoreLogController(db),
		sc:     NewScreenshotController(db),
	}
}

func (fc *flowController) LoadFlows(ctx context.Context) error {
	flows, err := fc.db.GetFlows(ctx)
	if err != nil {
		return fmt.Errorf("failed to load flows: %w", err)
	}

	for _, flow := range flows {
		fw, err := LoadFlowWorker(ctx, flow, flowWorkerCtx{
			db:     fc.db,
			cfg:    fc.cfg,
			docker: fc.docker,
			provs:  fc.provs,
			subs:   fc.subs,
			flowProviderControllers: flowProviderControllers{
				mlc:  fc.mlc,
				aslc: fc.aslc,
				alc:  fc.alc,
				slc:  fc.slc,
				tlc:  fc.tlc,
				vslc: fc.vslc,
				sc:   fc.sc,
			},
		})
		if err != nil {
			if errors.Is(err, ErrNothingToLoad) {
				continue
			}

			logrus.WithContext(ctx).WithError(err).Errorf("failed to load flow %d", flow.ID)
			continue
		}

		fc.flows[flow.ID] = fw
	}

	return nil
}

func (fc *flowController) CreateFlow(
	ctx context.Context,
	userID int64,
	input string,
	prvname provider.ProviderName,
	prvtype provider.ProviderType,
	functions *tools.Functions,
) (FlowWorker, error) {
	fc.mx.Lock()
	defer fc.mx.Unlock()

	// OSINT keyword routing.
	//
	// If the user prefixes their request with the literal "OSINT" keyword
	// (case-insensitive, optionally followed by ':', '-' or ','), strip the
	// keyword and prepend a directive that biases the primary agent toward
	// the OSINT specialist for the entire flow. The primary agent always has
	// the `osint` tool available in its function list (see GetPrimaryExecutor),
	// so this just steers it to use that tool instead of `pentester` / `search`.
	input = rewriteOsintInput(input)

	fw, err := NewFlowWorker(ctx, newFlowWorkerCtx{
		userID:    userID,
		input:     input,
		prvname:   prvname,
		prvtype:   prvtype,
		functions: functions,
		flowWorkerCtx: flowWorkerCtx{
			db:     fc.db,
			cfg:    fc.cfg,
			docker: fc.docker,
			provs:  fc.provs,
			subs:   fc.subs,
			flowProviderControllers: flowProviderControllers{
				mlc:  fc.mlc,
				aslc: fc.aslc,
				alc:  fc.alc,
				slc:  fc.slc,
				tlc:  fc.tlc,
				vslc: fc.vslc,
				sc:   fc.sc,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create flow worker: %w", err)
	}

	fc.flows[fw.GetFlowID()] = fw

	return fw, nil
}

func (fc *flowController) CreateAssistant(
	ctx context.Context,
	userID int64,
	flowID int64,
	input string,
	useAgents bool,
	prvname provider.ProviderName,
	prvtype provider.ProviderType,
	functions *tools.Functions,
) (AssistantWorker, error) {
	fc.mx.Lock()
	defer fc.mx.Unlock()

	var (
		fw  FlowWorker
		ok  bool
		err error
	)

	flowWorkerCtx := flowWorkerCtx{
		db:     fc.db,
		cfg:    fc.cfg,
		docker: fc.docker,
		provs:  fc.provs,
		subs:   fc.subs,
		flowProviderControllers: flowProviderControllers{
			mlc:  fc.mlc,
			aslc: fc.aslc,
			alc:  fc.alc,
			slc:  fc.slc,
			tlc:  fc.tlc,
			vslc: fc.vslc,
			sc:   fc.sc,
		},
	}

	newFlow := func() error {
		fw, err = NewFlowWorker(ctx, newFlowWorkerCtx{
			userID:        userID,
			input:         input,
			dryRun:        true,
			prvname:       prvname,
			prvtype:       prvtype,
			functions:     functions,
			flowWorkerCtx: flowWorkerCtx,
		})
		if err != nil {
			return fmt.Errorf("failed to create flow worker: %w", err)
		}

		fc.flows[fw.GetFlowID()] = fw
		flowID = fw.GetFlowID()
		fw.SetStatus(ctx, database.FlowStatusWaiting)

		return nil
	}

	loadFlow := func() error {
		flow, err := fc.db.UpdateFlowStatus(ctx, database.UpdateFlowStatusParams{
			ID:     flowID,
			Status: database.FlowStatusWaiting,
		})
		if err != nil {
			return fmt.Errorf("failed to renew flow %d status: %w", flowID, err)
		}

		fw, err = LoadFlowWorker(ctx, flow, flowWorkerCtx)
		if err != nil {
			return fmt.Errorf("failed to load flow %d: %w", flowID, err)
		}

		fc.flows[flowID] = fw

		return nil
	}

	if flowID == 0 {
		if err := newFlow(); err != nil {
			return nil, err
		}
	} else if fw, ok = fc.flows[flowID]; ok {
		status, err := fw.GetStatus(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get flow %d status: %w", flowID, err)
		}

		switch status {
		case database.FlowStatusCreated:
			return nil, fmt.Errorf("flow %d is not completed", flowID)
		case database.FlowStatusFinished, database.FlowStatusFailed:
			if err := loadFlow(); err != nil {
				return nil, err
			}
		case database.FlowStatusRunning, database.FlowStatusWaiting:
			break
		default:
			return nil, fmt.Errorf("flow %d is in unknown status: %s", flowID, status)
		}
	} else {
		if err := loadFlow(); err != nil {
			return nil, err
		}
	}

	if fw == nil { // just double check, this should never happen
		return nil, fmt.Errorf("unexpected error: flow %d not found", flowID)
	}

	aw, err := NewAssistantWorker(ctx, newAssistantWorkerCtx{
		userID:        userID,
		flowID:        flowID,
		input:         input,
		prvname:       prvname,
		prvtype:       prvtype,
		useAgents:     useAgents,
		functions:     functions,
		flowWorkerCtx: flowWorkerCtx,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create assistant: %w", err)
	}

	if err = fw.AddAssistant(ctx, aw); err != nil {
		return nil, fmt.Errorf("failed to add assistant to flow: %w", err)
	}

	return aw, nil
}

func (fc *flowController) ListFlows(ctx context.Context) []FlowWorker {
	fc.mx.Lock()
	defer fc.mx.Unlock()

	flows := make([]FlowWorker, 0)
	for _, flow := range fc.flows {
		flows = append(flows, flow)
	}

	sort.Slice(flows, func(i, j int) bool {
		return flows[i].GetFlowID() < flows[j].GetFlowID()
	})

	return flows
}

func (fc *flowController) GetFlow(ctx context.Context, flowID int64) (FlowWorker, error) {
	fc.mx.Lock()
	defer fc.mx.Unlock()

	flow, ok := fc.flows[flowID]
	if !ok {
		return nil, ErrFlowNotFound
	}

	return flow, nil
}

func (fc *flowController) StopFlow(ctx context.Context, flowID int64) error {
	fc.mx.Lock()
	defer fc.mx.Unlock()

	flow, ok := fc.flows[flowID]
	if !ok {
		return ErrFlowNotFound
	}

	err := flow.Stop(ctx)
	if err != nil {
		return fmt.Errorf("failed to stop flow %d: %w", flowID, err)
	}

	return nil
}

func (fc *flowController) FinishFlow(ctx context.Context, flowID int64) error {
	fc.mx.Lock()
	defer fc.mx.Unlock()

	flow, ok := fc.flows[flowID]
	if !ok {
		return ErrFlowNotFound
	}

	err := flow.Finish(ctx)
	if err != nil {
		return fmt.Errorf("failed to finish flow %d: %w", flowID, err)
	}

	delete(fc.flows, flowID)

	return nil
}

func (fc *flowController) RenameFlow(ctx context.Context, flowID int64, title string) error {
	fc.mx.Lock()
	defer fc.mx.Unlock()

	flow, ok := fc.flows[flowID]
	if !ok {
		return ErrFlowNotFound
	}

	return flow.Rename(ctx, title)
}

// osintKeyword is the literal prefix the user can place at the start of their
// flow input to opt into OSINT-specialist routing. Comparison is
// case-insensitive and tolerates an optional ':', '-' or ',' immediately after.
const osintKeyword = "OSINT"

// rewriteOsintInput detects an OSINT prefix in the user input and, if found,
// strips the keyword and prepends a directive that biases the primary agent
// toward the OSINT specialist (passive recon only). When no prefix is present
// the input is returned unchanged.
//
// Examples of inputs that trigger rewriting:
//
//	"OSINT example.com"
//	"osint: gather everything about acme corp"
//	"Osint - find leaked credentials for acme.io"
//
// The rewritten input keeps the user's original request verbatim and wraps it
// with an instruction the primary agent will see as the first user message in
// the flow's primary chain.
func rewriteOsintInput(input string) string {
	trimmed := strings.TrimSpace(input)
	if len(trimmed) < len(osintKeyword) {
		return input
	}
	if !strings.EqualFold(trimmed[:len(osintKeyword)], osintKeyword) {
		return input
	}

	// The character right after the keyword must be a separator (whitespace,
	// ':', '-' or ','), otherwise we'd match words like "OSINTOOLS".
	rest := trimmed[len(osintKeyword):]
	if len(rest) > 0 {
		switch rest[0] {
		case ' ', '\t', '\n', '\r', ':', '-', ',':
			// ok, real OSINT prefix
		default:
			return input
		}
	}

	rest = strings.TrimLeft(rest, " \t\n\r:,-")
	rest = strings.TrimSpace(rest)

	const directive = "This task is an OSINT (open-source intelligence) engagement. " +
		"Use the `osint` tool (passive open-source intelligence specialist) for ALL " +
		"information gathering. Do NOT use the `pentester` tool against the target, " +
		"do NOT call the `terminal` tool against the target, " +
		"and do NOT initiate any active probing of the target's infrastructure. " +
		"Only passive sources are authorized: WHOIS, " +
		"certificate transparency, passive DNS, public code repositories, archives, " +
		"breach indexes, and search engines.\n\nUser request: "

	if rest == "" {
		return directive + "(no further details provided)"
	}
	return directive + rest
}
