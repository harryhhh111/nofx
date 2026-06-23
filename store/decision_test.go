package store

import (
	"strings"
	"testing"
)

func TestDecisionDigestIncludesReadableJudgementSignals(t *testing.T) {
	record := testDecisionRecordDBWithJudgementJSON()

	digest := record.toDigest()
	if digest.JudgementSummary == "" {
		t.Fatal("expected digest judgement summary")
	}
	assertReadableJudgementSummary(t, digest.JudgementSummary)
}

func TestDecisionRecordIncludesReadableJudgementSignals(t *testing.T) {
	record := testDecisionRecordDBWithJudgementJSON().toRecord()

	if record.JudgementSummary == "" {
		t.Fatal("expected decision record judgement summary")
	}
	assertReadableJudgementSummary(t, record.JudgementSummary)
}

func testDecisionRecordDBWithJudgementJSON() *DecisionRecordDB {
	return &DecisionRecordDB{
		TraderID:    "trader-1",
		CycleNumber: 7,
		CotSummary:  "本轮完成市场评估，但没有满足开仓条件的信号。",
		DecisionJSON: `{
  "user_decision_summary": {
    "status": "no_trade",
    "headline": "本轮完成市场评估，但没有满足开仓条件的信号。",
    "symbols": [
      {
        "symbol": "BTCUSDT",
        "decision": "skip",
        "reason": "行情偏空，但还没有达到策略要求的开仓强度。"
      }
    ]
  },
  "setup_evaluations": [
    {
      "symbol": "BTCUSDT",
      "setup": "no_trade_threshold_not_met",
      "eligible": false,
      "primary": {"timeframe": "15m", "score": -47.5, "eligible": true},
      "entry": {"timeframe": "15m", "score": -46, "eligible": true},
      "confirmations": [{"timeframe": "1h", "score": -12, "eligible": true}]
    }
  ]
}`,
	}
}

func assertReadableJudgementSummary(t *testing.T, summary string) {
	t.Helper()
	for _, expected := range []string{"BTCUSDT", "偏空", "主周期 -47.5", "入场 -46.0", "1h -12.0", "行情偏空"} {
		if !strings.Contains(summary, expected) {
			t.Fatalf("expected judgement summary to contain %q, got %q", expected, summary)
		}
	}
	if strings.Contains(summary, "setup") || strings.Contains(summary, "no_trade") {
		t.Fatalf("judgement summary should not expose setup internals, got %q", summary)
	}
}
