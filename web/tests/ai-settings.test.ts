import { describe, expect, it } from "vitest";
import { validateCapabilityDraft } from "@/components/Settings/ai-setting-state";

describe("AI capability settings", () => {
  it("requires provider and model together", () => {
    expect(validateCapabilityDraft({ providerId: "provider", model: "", dimensions: 0 }, false)).toBe("model-required");
    expect(validateCapabilityDraft({ providerId: "", model: "model", dimensions: 0 }, false)).toBe("provider-required");
  });

  it("requires disclosure before external embeddings", () => {
    expect(validateCapabilityDraft({ providerId: "provider", model: "embed", dimensions: 768 }, true, false)).toBe("disclosure-required");
    expect(validateCapabilityDraft({ providerId: "provider", model: "embed", dimensions: 768 }, true, true)).toBeUndefined();
  });

  it("rejects invalid embedding dimensions", () => {
    expect(validateCapabilityDraft({ providerId: "provider", model: "embed", dimensions: -1 }, true, true)).toBe("dimensions-invalid");
  });
});
