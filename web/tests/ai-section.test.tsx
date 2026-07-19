import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import AISection from "@/components/Settings/AISection";

const mocks = vi.hoisted(() => ({
  saveSetting: vi.fn(async () => true),
  fetchSetting: vi.fn(async () => undefined),
  testInstanceAISetting: vi.fn(async () => ({ ready: true, category: "", message: "" })),
  toastSuccess: vi.fn(),
  state: { aiSetting: {} as Record<string, unknown> },
}));

vi.mock("@/contexts/InstanceContext", () => ({
  useInstance: () => ({ aiSetting: mocks.state.aiSetting, fetchSetting: mocks.fetchSetting }),
}));

vi.mock("@/components/Settings/useInstanceSettingUpdater", () => ({
  default: () => mocks.saveSetting,
  buildInstanceSettingName: () => "instance/settings/AI",
}));

vi.mock("@/connect", () => ({
  instanceServiceClient: { testInstanceAISetting: mocks.testInstanceAISetting },
}));

vi.mock("react-hot-toast", () => ({
  toast: { success: mocks.toastSuccess, error: vi.fn() },
}));

vi.mock("@/utils/i18n", () => ({
  useTranslate: () => (key: string) => key,
}));

describe("<AISection>", () => {
  beforeEach(() => {
    mocks.state.aiSetting = {
      providers: [],
      transcription: undefined,
      generation: undefined,
      embedding: undefined,
      externalProcessingAcknowledged: false,
      readiness: undefined,
    };
  });

  it("renders additive generation, embedding disclosure, and unchanged transcription controls", () => {
    render(<AISection />);

    expect(screen.getByText("setting.ai.generation-title")).toBeInTheDocument();
    expect(screen.getByText("setting.ai.embedding-disclosure-title")).toBeInTheDocument();
    expect(screen.getByText("setting.ai.transcription-title")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "setting.ai.test-generation" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "setting.ai.test-embedding" })).toBeDisabled();
  });

  it("runs a bounded capability test without loading a provider key", async () => {
    mocks.state.aiSetting = {
      ...mocks.state.aiSetting,
      providers: [
        {
          id: "provider",
          title: "Provider",
          type: 1,
          endpoint: "https://api.openai.com/v1",
          apiKey: "",
          apiKeySet: true,
          apiKeyHint: "sk...test",
          allowPrivateNetwork: false,
        },
      ],
      generation: { providerId: "provider", model: "model" },
    };
    render(<AISection />);

    fireEvent.click(screen.getByRole("button", { name: "setting.ai.test-generation" }));
    await waitFor(() => expect(mocks.testInstanceAISetting).toHaveBeenCalled());
    expect(mocks.testInstanceAISetting.mock.calls[0][0].provider.apiKey).toBe("");
    expect(mocks.toastSuccess).toHaveBeenCalledWith("setting.ai.test-succeeded");
  });
});
