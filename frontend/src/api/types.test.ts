import { describe, expect, test } from "vitest";

import { networkFromApi, networkToApi, TRANSMISSION_OPTIONS } from "./types";

describe("Naive network conversion", () => {
  test("maps API empty or unknown values to the UI auto mode", () => {
    expect(networkFromApi(undefined)).toBe("both");
    expect(networkFromApi("")).toBe("both");
    expect(networkFromApi("both")).toBe("both");
  });

  test("omits UI auto mode from API payloads", () => {
    expect(networkToApi("both")).toBeUndefined();
    expect(networkToApi("tcp")).toBe("tcp");
    expect(networkToApi("udp")).toBe("udp");
  });
});

test("exposes HTTPUpgrade as a VLESS transport", () => {
  expect(TRANSMISSION_OPTIONS).toContainEqual({ value: "httpupgrade", label: "HTTPUpgrade" });
});
