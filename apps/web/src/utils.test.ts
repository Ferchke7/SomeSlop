import { describe, expect, it } from "vitest";
import { clock, daysBetween, formatDuration } from "./App";

describe("time formatting", () => {
  it("formats long monthly workloads", () => {
    expect(formatDuration(Math.ceil(500 * 3600 / 31))).toBe("16 ч 8 мин");
    expect(formatDuration(Math.ceil(600 * 3600 / 31))).toBe("19 ч 21 мин");
  });

  it("formats the live timer", () => {
    expect(clock(65)).toBe("01:05");
    expect(clock(-5)).toBe("00:00");
  });

  it("counts both period boundaries", () => {
    expect(daysBetween("2026-07-01", "2026-07-31")).toBe(31);
  });
});
