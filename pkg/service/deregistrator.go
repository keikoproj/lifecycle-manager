package service

import (
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/elb"
	"github.com/aws/aws-sdk-go/service/elbv2"
	"github.com/keikoproj/lifecycle-manager/pkg/log"
)

type Deregistrator struct {
	start                    chan bool
	targetDeregisteredCount  int
	classicDeregisteredCount int
}

type DeregistrationError struct {
	Error     error
	Target    string
	Instances []string
	Type      string
}

func (mgr *Manager) newDeregistrator() {
	for {
		select {
		case <-mgr.deregistrator.start:
			log.Info("deregistrator starting")
			mgr.deregistrator.targetDeregisteredCount = 0
			mgr.deregistrator.classicDeregisteredCount = 0
			mgr.targets.Range(func(k, v interface{}) bool {
				var (
					targets = v.([]*Target)
				)
				waitJitter(IterationJitterRangeSeconds)
				mgr.DeregisterTargets(targets)
				return true
			})
			log.Infof("deregistrator> deregistered %v instances from target groups and %v instances from classic-elbs", mgr.deregistrator.targetDeregisteredCount, mgr.deregistrator.classicDeregisteredCount)
		}
	}
}

func (m *Manager) DeregisterTargets(targets []*Target) {
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
					Type:      TargetTypeClassicELB.String(),
				}
				m.deregisterErrors <- deregistrationErr
			}
			m.deregistrator.classicDeregisteredCount += len(instances)
			for _, instance := range instances {
				m.RemoveTargetByInstance(target.TargetId, instance)
			}
		case TargetTypeTargetGroup:
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
					Type:      TargetTypeTargetGroup.String(),
				}
				m.deregisterErrors <- deregistrationErr
			}
			m.deregistrator.targetDeregisteredCount += len(instances)
			for _, instance := range instances {
				m.RemoveTargetByInstance(target.TargetId, instance)
			}
		}
	}
}
