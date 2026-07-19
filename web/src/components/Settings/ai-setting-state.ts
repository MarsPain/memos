export type CapabilityDraft = {
  providerId: string;
  model: string;
  dimensions: number;
};

export type CapabilityValidationError = "provider-required" | "model-required" | "dimensions-invalid" | "disclosure-required";

export const validateCapabilityDraft = (
  draft: CapabilityDraft,
  embedding: boolean,
  externalProcessingAcknowledged = false,
): CapabilityValidationError | undefined => {
  if (!draft.providerId && draft.model.trim()) return "provider-required";
  if (draft.providerId && !draft.model.trim()) return "model-required";
  if (embedding && (draft.dimensions < 0 || draft.dimensions > 65536)) return "dimensions-invalid";
  if (embedding && draft.providerId && !externalProcessingAcknowledged) return "disclosure-required";
  return undefined;
};
