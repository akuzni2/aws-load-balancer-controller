package ingress

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	networking "k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/aws-alb-ingress-controller/pkg/annotations"
)

const (
	defaultAuthType                     = AuthTypeNone
	defaultAuthScope                    = "openid"
	defaultAuthSessionCookieName        = "AWSELBAuthSessionCookie"
	defaultAuthSessionTimeout           = 604800
	defaultAuthOnUnauthenticatedRequest = "authenticate"

	magicServicePortUseAnnotation = "use-annotation"
)

// An enhanced version of Ingress backend.
// It contains additional routing conditions we parsed from annotations.
// Also, when magic string `use-annotation` is specified as backend, the actions will be parsed from annotations as well.
type EnhancedBackend struct {
	Conditions []RuleCondition
	Action     Action
}

// EnhancedBackendBuilder is capable of build  EnhancedBackend for Ingress backend.
type EnhancedBackendBuilder interface {
	Build(ctx context.Context, ing *networking.Ingress, backend networking.IngressBackend) (EnhancedBackend, error)
}

// NewDefaultEnhancedBackendBuilder constructs new defaultEnhancedBackendBuilder.
func NewDefaultEnhancedBackendBuilder(annotationParser annotations.Parser) *defaultEnhancedBackendBuilder {
	return &defaultEnhancedBackendBuilder{
		annotationParser: annotationParser,
	}
}

var _ EnhancedBackendBuilder = &defaultEnhancedBackendBuilder{}

// default implementation for defaultEnhancedBackendBuilder
type defaultEnhancedBackendBuilder struct {
	annotationParser annotations.Parser
}

func (b *defaultEnhancedBackendBuilder) Build(ctx context.Context, ing *networking.Ingress, backend networking.IngressBackend) (EnhancedBackend, error) {
	conditions, err := b.buildConditions(ctx, ing.Annotations, backend.ServiceName)
	if err != nil {
		return EnhancedBackend{}, err
	}

	var action Action
	if backend.ServicePort.String() == magicServicePortUseAnnotation {
		action, err = b.buildActionViaAnnotation(ctx, ing.Annotations, backend.ServiceName)
		if err != nil {
			return EnhancedBackend{}, err
		}
	} else {
		action = b.buildActionViaServiceAndServicePort(ctx, backend.ServiceName, backend.ServicePort)
	}

	return EnhancedBackend{
		Conditions: conditions,
		Action:     action,
	}, nil
}

func (b *defaultEnhancedBackendBuilder) buildConditions(ctx context.Context, ingAnnotation map[string]string, svcName string) ([]RuleCondition, error) {
	var conditions []RuleCondition
	annotationKey := fmt.Sprintf("conditions.%v", svcName)
	_, err := b.annotationParser.ParseJSONAnnotation(annotationKey, &conditions, ingAnnotation)
	if err != nil {
		return nil, err
	}
	for _, condition := range conditions {
		if err := condition.validate(); err != nil {
			return nil, err
		}
	}
	return conditions, nil
}

func (b *defaultEnhancedBackendBuilder) buildActionViaAnnotation(ctx context.Context, ingAnnotation map[string]string, svcName string) (Action, error) {
	action := Action{}
	annotationKey := fmt.Sprintf("actions.%v", svcName)
	exists, err := b.annotationParser.ParseJSONAnnotation(annotationKey, &action, ingAnnotation)
	if err != nil {
		return Action{}, err
	}
	if !exists {
		return Action{}, errors.Errorf("missing %v configuration", annotationKey)
	}
	if err := action.validate(); err != nil {
		return Action{}, err
	}

	// normalize forward action via TargetGroupARN.
	if action.Type == ActionTypeForward && action.TargetGroupARN != nil {
		action.ForwardConfig = &ForwardActionConfig{
			TargetGroups: []TargetGroupTuple{
				{
					TargetGroupARN: action.TargetGroupARN,
				},
			},
		}
		action.TargetGroupARN = nil
	}
	return action, nil
}

func (b *defaultEnhancedBackendBuilder) buildActionViaServiceAndServicePort(ctx context.Context, svcName string, svcPort intstr.IntOrString) Action {
	return Action{
		Type: ActionTypeForward,
		ForwardConfig: &ForwardActionConfig{
			TargetGroups: []TargetGroupTuple{
				{
					ServiceName: &svcName,
					ServicePort: &svcPort,
				},
			},
		},
	}
}