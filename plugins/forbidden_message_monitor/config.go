// 📌 影响范围：定义违禁消息检测引擎的纯内存规则配置；不读取环境变量或外部服务。
package forbiddenmessagemonitor

import (
	"errors"
	"fmt"
	"math"
)

// RiskBand 表示规则评分后的检测分流。
type RiskBand string

const (
	RiskBandLow    RiskBand = "low"
	RiskBandMedium RiskBand = "medium"
	RiskBandHigh   RiskBand = "high"
)

// WeightedKeyword 定义关键词及其风险权重。
type WeightedKeyword struct {
	Text   string
	Weight float64
}

// CombinationRule 定义所有词同时出现时的组合加成。
type CombinationRule struct {
	Terms []string
	Bonus float64
}

// EngineConfig 定义纯内存检测规则与评分阈值。
type EngineConfig struct {
	HardKeywords             []string
	WeightedKeywords         []WeightedKeyword
	SafeKeywords             []WeightedKeyword
	Combinations             []CombinationRule
	LowThreshold             float64
	HighThreshold            float64
	LengthNormalizationRunes int
}

// DefaultEngineConfig 返回可安全运行的保守默认配置。
// @param 无。
// @returns 阈值与长度归一化参数完整的默认配置。
// ⚠️副作用说明：无。
func DefaultEngineConfig() EngineConfig {
	result := EngineConfig{
		LowThreshold:             20,
		HighThreshold:            60,
		LengthNormalizationRunes: 80,
	}

	// >>> 数据演变示例
	// 1. 空输入 -> 默认低/高阈值20/60 -> 可直接构造引擎。
	// 2. 空输入 -> 长度归一化基准80 -> 长消息评分保持有界。
	return result
}

// validate 校验阈值、权重与组合规则，避免不可达分流。
// @param config：待校验配置。
// @returns 配置合法时nil，否则返回具体字段错误。
// ⚠️副作用说明：无。
func (config EngineConfig) validate() error {
	// [决策理由] 分流阈值必须有序且非负，否则低中高区间含义不确定。
	if !finite(config.LowThreshold) || !finite(config.HighThreshold) || config.LowThreshold < 0 || config.HighThreshold <= config.LowThreshold {
		return errors.New("high threshold must be greater than non-negative low threshold")
	}
	// [决策理由] 归一化基准必须为正，避免除零。
	if config.LengthNormalizationRunes <= 0 {
		return errors.New("length normalization runes must be positive")
	}
	for index, keyword := range append(append([]WeightedKeyword(nil), config.WeightedKeywords...), config.SafeKeywords...) {
		// [决策理由] 空词会匹配所有消息，非有限权重会污染评分。
		if keyword.Text == "" || !finite(keyword.Weight) || keyword.Weight < 0 {
			return fmt.Errorf("keyword %d must have text and non-negative weight", index)
		}
	}
	for index, combination := range config.Combinations {
		// [决策理由] 空组合、空词或非有限加成会造成无条件或不可解释评分。
		if len(combination.Terms) == 0 || !finite(combination.Bonus) {
			return fmt.Errorf("combination %d must have terms and a finite bonus", index)
		}
		for _, term := range combination.Terms {
			// [决策理由] 空字符串会匹配所有消息，破坏组合同时命中的语义。
			if term == "" {
				return fmt.Errorf("combination %d contains an empty term", index)
			}
		}
	}

	// >>> 数据演变示例
	// 1. 阈值20/60且归一化基准80 -> 全部约束通过 -> nil。
	// 2. 阈值60/20 -> 区间倒置 -> 返回阈值错误。
	return nil
}

// finite 判断浮点配置是否为有限数。
// @param value：待检查浮点值。
// @returns 非NaN且非正负无穷时为true。
// ⚠️副作用说明：无。
func finite(value float64) bool {
	result := !math.IsNaN(value) && !math.IsInf(value, 0)

	// >>> 数据演变示例
	// 1. 20 -> 非NaN且非无穷 -> true。
	// 2. NaN -> IsNaN命中 -> false。
	return result
}
