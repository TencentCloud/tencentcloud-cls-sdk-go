package tencentcloud_cls_sdk_go

import (
	"context"
	"errors"
	"fmt"
	"time"

	polarisapi "git.woa.com/polaris/polaris-go/v2/api"
	"git.woa.com/polaris/polaris-go/v2/pkg/model"
)

// PolarisResolverConfig 北极星 Resolver 配置。
//
// 基于 polaris-go v2（git.woa.com/polaris/polaris-go/v2）实现，
// 用于将 CLS 上报的目标 Endpoint 通过北极星做服务发现与负载均衡。
type PolarisResolverConfig struct {
	// Namespace 必填，北极星命名空间，例如 "Production"。
	Namespace string
	// Service 必填，北极星服务名（对应 CLS 网关在北极星上注册的服务名）。
	Service string
	// Scheme 可选，"http" 或 "https"，默认 "http"。
	Scheme string
	// FallbackHost 可选：当北极星服务发现失败时的兜底地址，
	// 例如 "ap-guangzhou.cls.tencentcs.com"。为空则在服务发现失败时直接返回错误。
	FallbackHost string
	// Timeout 可选，单次服务发现的超时时间。<=0 时使用北极星的默认配置。
	Timeout time.Duration
	// LbPolicy 可选，负载均衡算法，如 "weightedRandom"、"ringHash" 等。
	// 为空时使用北极星默认策略。
	LbPolicy string
	// ConsumerAPI 可选：如果你已经在其他地方创建了 ConsumerAPI，可直接注入复用。
	// 为空时内部会通过 polarisapi.NewConsumerAPI() 使用默认配置文件（./polaris.yaml）创建。
	ConsumerAPI polarisapi.ConsumerAPI
	// EnableReport 是否在每次请求结束后把调用结果（成功/失败、耗时、状态码）
	// 上报回北极星，供其做故障剔除、负载均衡与熔断。默认 false（不上报）。
	// 仅当你希望北极星根据 CLS 上报请求的真实结果动态调整实例健康度时才开启。
	EnableReport bool
}

// PolarisResolver 基于北极星（polaris-go v2）的 EndpointResolver 实现。
type PolarisResolver struct {
	cfg      PolarisResolverConfig
	consumer polarisapi.ConsumerAPI
	// 是否是内部创建的 consumer；只有内部创建的才由本 Resolver 负责销毁
	ownConsumer bool
}

// NewPolarisResolver 创建一个新的北极星 Resolver。
// 如果 cfg.ConsumerAPI 为空，将通过 polarisapi.NewConsumerAPI() 使用默认配置文件创建。
func NewPolarisResolver(cfg PolarisResolverConfig) (*PolarisResolver, error) {
	if cfg.Namespace == "" {
		return nil, errors.New("polaris resolver: Namespace cannot be empty")
	}
	if cfg.Service == "" {
		return nil, errors.New("polaris resolver: Service cannot be empty")
	}
	if cfg.Scheme == "" {
		cfg.Scheme = "http"
	}

	r := &PolarisResolver{cfg: cfg}
	if cfg.ConsumerAPI != nil {
		r.consumer = cfg.ConsumerAPI
		r.ownConsumer = false
	} else {
		consumer, err := polarisapi.NewConsumerAPI()
		if err != nil {
			return nil, fmt.Errorf("polaris resolver: create consumer api failed: %w", err)
		}
		r.consumer = consumer
		r.ownConsumer = true
	}
	return r, nil
}

// Close 释放内部持有的 ConsumerAPI（仅当由本 Resolver 内部创建时才会销毁）。
func (r *PolarisResolver) Close() {
	if r.ownConsumer && r.consumer != nil {
		r.consumer.Destroy()
		r.consumer = nil
	}
}

// Resolve 实现 EndpointResolver 接口，返回一个由北极星选出的实例地址。
func (r *PolarisResolver) Resolve(_ context.Context) (*ResolvedEndpoint, Reporter, error) {
	req := &polarisapi.GetOneInstanceRequest{}
	req.Namespace = r.cfg.Namespace
	req.Service = r.cfg.Service
	if r.cfg.Timeout > 0 {
		req.SetTimeout(r.cfg.Timeout)
	}
	if r.cfg.LbPolicy != "" {
		req.LbPolicy = r.cfg.LbPolicy
	}

	resp, err := r.consumer.GetOneInstance(req)
	if err != nil || resp == nil || len(resp.GetInstances()) == 0 {
		// 服务发现失败，走兜底
		if r.cfg.FallbackHost != "" {
			return &ResolvedEndpoint{
				Host:   r.cfg.FallbackHost,
				Scheme: r.cfg.Scheme,
			}, nil, nil
		}
		if err == nil {
			err = errors.New("polaris resolver: no available instance")
		}
		return nil, nil, err
	}

	inst := resp.GetInstances()[0]
	host := fmt.Sprintf("%s:%d", inst.GetHost(), inst.GetPort())

	// 默认不上报调用结果；仅当显式开启 EnableReport 时才返回 reporter。
	var reporter Reporter
	if r.cfg.EnableReport {
		reporter = &polarisReporter{
			consumer: r.consumer,
			instance: inst,
		}
	}
	return &ResolvedEndpoint{
		Host:   host,
		Scheme: r.cfg.Scheme,
	}, reporter, nil
}

// polarisReporter 用于把一次调用结果上报回北极星，供负载均衡/熔断使用。
type polarisReporter struct {
	consumer polarisapi.ConsumerAPI
	instance model.Instance
}

// Report 实现 Reporter 接口。
func (p *polarisReporter) Report(err error, statusCode int, cost time.Duration) {
	if p == nil || p.consumer == nil || p.instance == nil {
		return
	}
	result := &polarisapi.ServiceCallResult{}
	result.SetCalledInstance(p.instance)
	result.SetDelay(cost)

	// 5xx / 网络错误 判为失败；2xx/4xx 判为成功（4xx 属于客户端错误，不应影响实例健康度）
	if err != nil || statusCode >= 500 {
		result.SetRetStatus(model.RetFail)
	} else {
		result.SetRetStatus(model.RetSuccess)
	}
	result.SetRetCode(int32(statusCode))

	// 忽略上报错误，避免影响主流程
	_ = p.consumer.UpdateServiceCallResult(result)
}
