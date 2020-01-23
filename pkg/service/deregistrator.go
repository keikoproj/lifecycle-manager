package service

import (
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
)

type Deregistrator struct {
	targetDeregisteredCount  int
	classicDeregisteredCount int
	errors                   chan DeregistrationError
}

func (d *Deregistrator) AddClassicDeregistration(val int)     { d.classicDeregisteredCount += val }
func (d *Deregistrator) AddTargetGroupDeregistration(val int) { d.targetDeregisteredCount += val }

type DeregistrationError struct {
	Error     error
	Target    string
	Instances []string
	Type      TargetType
}

func (mgr *Manager) startDeregistrator(d *Deregistrator) {
	mgr.deregistrationMu.Lock()
	defer mgr.deregistrationMu.Unlock()

	mgr.targets.Range(func(k, v interface{}) bool {
		var (
			targets = v.([]*Target)
		)
		waitJitter(IterationJitterRangeSeconds)
		mgr.DeregisterTargets(targets, d)
		return true
	})
	if d.targetDeregisteredCount == 0 && d.classicDeregisteredCount == 0 {
		log.Infof("deregistrator> no active targets for deregistration")
	} else {
		log.Infof("deregistrator> deregistered %v instances from target groups and %v instances from classic-elbs", d.targetDeregisteredCount, d.classicDeregisteredCount)
	}

}

func (m *Manager) DeregisterTargets(targets []*Target, d *Deregistrator) {
	var (
		elbClient   = m.authenticator.ELBClient
		elbv2Client = m.authenticator.ELBv2Client
	)

	for _, target := range targets {
		mapping := m.GetTargetMapping(target.TargetId)
		instances := m.GetTargetInstanceIds(target.TargetId)
		if len(instances) == 0 || len(mapping) == 0 {
			continue
		}

		switch target.Type {
		case TargetTypeClassicELB:
			log.Infof("deregistrator> deregistering %+v from %v", instances, target.TargetId)
			err := deregisterInstances(elbClient, target.TargetId, instances)
			if err != nil {
				if awsErr, ok := err.(awserr.Error); ok {
					if awsErr.Code() == elb.ErrCodeAccessPointNotFoundException {
						log.Warnf("deregistrator> ELB %v not found, skipping", target.TargetId)
						continue
					} else if awsErr.Code() == elb.ErrCodeInvalidEndPointException {
						log.Warnf("deregistrator> ELB targets %v not found in %v, skipping", instances, target.TargetId)
						continue
					}
				}
				log.Errorf("deregistrator> target deregistration failed: %v", err)
				deregistrationErr := DeregistrationError{
					Error:     err,
					Target:    target.TargetId,
					Instances: instances,
					Type:      TargetTypeClassicELB,
				}
				d.errors <- deregistrationErr
			}
			d.AddClassicDeregistration(len(instances))
			for _, instance := range instances {
				m.RemoveTargetByInstance(target.TargetId, instance)
			}
		case TargetTypeTargetGroup:
			log.Infof("deregistrator> deregistering %+v from %v", instances, target.TargetId)
			err := deregisterTargets(elbv2Client, target.TargetId, mapping)
			if err != nil {
				if awsErr, ok := err.(awserr.Error); ok {
					if awsErr.Code() == elbv2.ErrCodeTargetGroupNotFoundException {
						log.Warnf("deregistrator> target group %v not found, skipping", target.TargetId)
						continue
					} else if awsErr.Code() == elbv2.ErrCodeInvalidTargetException {
						log.Warnf("deregistrator> target %v not found in target group %v, skipping", instances, target.TargetId)
						continue
					}
				}
				log.Errorf("deregistrator> target deregistration failed: %v", err)
				deregistrationErr := DeregistrationError{
					Error:     err,
					Target:    target.TargetId,
					Instances: instances,
					Type:      TargetTypeTargetGroup,
				}
				d.errors <- deregistrationErr
			}
			d.AddTargetGroupDeregistration(len(instances))
			for _, instance := range instances {
				m.RemoveTargetByInstance(target.TargetId, instance)
			}
		}
	}
}
