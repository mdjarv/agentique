import { expect, type Page, test } from "@playwright/test";
import {
  immediate,
  resetFixture,
  result,
  type Scenario,
  type SeedRequest,
  seedFixture,
  TEST_PROJECT,
  TEST_PROJECT_ID,
  text,
  thinking,
  toolResult,
  toolUse,
  withDelay,
} from "./fixtures";
import { navigateToSession, sendQuery, waitForState } from "./helpers";

// Teams were renamed to channels, and the per-session "Team" tab was removed —
// channel state now shows up in the session action menu (Create/Join flips to
// Leave once you are in one), on the /teams page, and as messages delivered
// into the recipient session's own chat. These specs follow that UI.

// --- Constants ---

const SESSION_1_ID = "eee00070-0000-4000-8000-000000000070";
const SESSION_2_ID = "eee00071-0000-4000-8000-000000000071";
const SESSION_1_NAME = "Lead Agent";
const SESSION_2_NAME = "Worker Agent";
const CHANNEL_NAME = "Alpha Squad";
const DELEGATED_WORK = "Please write tests for the main function in src/app.ts";

// --- Scenarios ---

/** Session 1 reads a file, then sends a message to Worker Agent. */
const SCENARIO_SEND_MESSAGE: Scenario = {
  events: [
    immediate(thinking("I should delegate the test work to Worker Agent.")),
    withDelay(20, text("Let me check the code first, then delegate.")),
    withDelay(
      30,
      toolUse("team-read-001", "Read", { file_path: "/tmp/fixture-project/src/app.ts" }),
    ),
    withDelay(20, toolResult("team-read-001", "export function main() { return 42; }")),
    withDelay(20, text("Delegating test writing to Worker Agent.")),
    withDelay(
      30,
      // Must be the MCP-qualified name. The pipeline routes on
      // AgentiqueSendMessageTool / ChannelSendMessageTool
      // ("mcp__agentique__SendMessage", "mcp__agentique-channel__SendMessage");
      // a bare "SendMessage" renders as a tool call and routes nothing.
      toolUse("team-send-001", "mcp__agentique__SendMessage", {
        to: SESSION_2_NAME,
        content: DELEGATED_WORK,
      }),
    ),
    withDelay(20, toolResult("team-send-001", "Message sent to Worker Agent")),
    withDelay(20, text("Delegated to the worker. Waiting for their response.")),
    withDelay(10, result()),
  ],
};

/** Simple read-only scenario for the worker session. */
const SCENARIO_WORKER_IDLE: Scenario = {
  events: [
    immediate(thinking("Ready to receive work.")),
    withDelay(20, text("Standing by for instructions.")),
    withDelay(10, result()),
  ],
};

// --- Seed helpers ---

function teamSeed(): SeedRequest {
  return {
    projects: [TEST_PROJECT],
    sessions: [
      {
        id: SESSION_1_ID,
        projectId: TEST_PROJECT_ID,
        name: SESSION_1_NAME,
        workDir: "/tmp/fixture-project",
        live: true,
        behavior: [SCENARIO_SEND_MESSAGE],
        // fullAuto, not auto: mcp__ tools classify as "mcp", which is not in
        // autoSafeCategories, so under auto the replay parks on an approval
        // banner and never reaches the delegation. Permission behaviour is
        // covered in permissions.spec.ts; this file is about routing.
        autoApproveMode: "fullAuto",
      },
      {
        id: SESSION_2_ID,
        projectId: TEST_PROJECT_ID,
        name: SESSION_2_NAME,
        workDir: "/tmp/fixture-project",
        live: true,
        behavior: [SCENARIO_WORKER_IDLE],
        autoApproveMode: "auto",
      },
    ],
  };
}

// --- UI helpers ---

/** Open the session header overflow menu (MoreHorizontal button). */
async function openOverflowMenu(page: Page) {
  const header = page.locator("header");
  await expect(header).toBeVisible({ timeout: 5_000 });
  // The overflow trigger is the last button in the header actions and carries
  // no accessible name, just an icon. Retry the click: the trigger toggles, so
  // a click that lands while a previous menu is still closing shuts it again.
  const trigger = header.locator("button").last();
  const items = page.getByRole("menuitem");
  await expect(async () => {
    if ((await items.count()) === 0) {
      await trigger.click();
    }
    await expect(items.first()).toBeVisible({ timeout: 1_000 });
  }).toPass({ timeout: 10_000 });
}

/** Create a channel from the currently active session. */
async function createChannelFromUI(page: Page, name: string, role = "") {
  await openOverflowMenu(page);
  await page.getByRole("menuitem", { name: "Create channel..." }).click();
  await page.getByPlaceholder("Channel name").fill(name);
  if (role) {
    await page.getByPlaceholder("Your role (optional)").fill(role);
  }
  await page.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByText("Channel created")).toBeVisible({ timeout: 5_000 });
}

/** Join an existing channel from the currently active session. */
async function joinChannelFromUI(page: Page, role = "") {
  await openOverflowMenu(page);
  await page.getByRole("menuitem", { name: "Join channel..." }).click();
  if (role) {
    await page.getByPlaceholder("Your role (optional)").fill(role);
  }
  await page.getByRole("button", { name: "Join" }).click();
  await expect(page.getByText("Joined channel")).toBeVisible({ timeout: 5_000 });
}

/** Dismiss the overflow menu and wait for it to actually be gone. */
async function closeOverflowMenu(page: Page) {
  await page.keyboard.press("Escape");
  // Without waiting for the close, the next openOverflowMenu click lands on a
  // still-open menu and toggles it shut instead of reopening it.
  await expect(page.getByRole("menuitem").first()).toHaveCount(0, { timeout: 5_000 });
}

/**
 * Assert whether the active session is in a channel. The action menu is the
 * observable: membership replaces Create/Join with Leave.
 */
async function expectChannelMembership(page: Page, member: boolean) {
  await openOverflowMenu(page);
  const leave = page.getByRole("menuitem", { name: "Leave channel" });
  const create = page.getByRole("menuitem", { name: "Create channel..." });
  if (member) {
    await expect(leave).toBeVisible({ timeout: 5_000 });
    await expect(create).toHaveCount(0);
  } else {
    await expect(create).toBeVisible({ timeout: 5_000 });
    await expect(leave).toHaveCount(0);
  }
  await closeOverflowMenu(page);
}

// --- Tests ---

test.beforeEach(async ({ request }) => {
  await resetFixture(request);
});

test.describe("Channel lifecycle", () => {
  test("create channel from a session", async ({ page, request }) => {
    await seedFixture(request, teamSeed());
    await navigateToSession(page, SESSION_1_NAME);

    await createChannelFromUI(page, CHANNEL_NAME, "lead");

    await expectChannelMembership(page, true);
  });

  test("second session can join existing channel", async ({ page, request }) => {
    await seedFixture(request, teamSeed());

    await navigateToSession(page, SESSION_1_NAME);
    await createChannelFromUI(page, CHANNEL_NAME, "lead");

    await navigateToSession(page, SESSION_2_NAME);
    await joinChannelFromUI(page, "worker");

    await expectChannelMembership(page, true);
  });

  test("channel creator is marked as lead once a worker joins", async ({ page, request }) => {
    await seedFixture(request, teamSeed());

    await navigateToSession(page, SESSION_1_NAME);
    await createChannelFromUI(page, CHANNEL_NAME, "lead");
    await navigateToSession(page, SESSION_2_NAME);
    await joinChannelFromUI(page, "worker");

    // The project sidebar does not group by channel — membership surfaces as a
    // worker-count badge on the creator's row. (The channel-grouped sidebar is
    // the /teams variant, which only populates after its own subscription
    // settles, so asserting it here races the page load.)
    await expect(page.getByTitle("Lead of 1 worker")).toBeVisible({ timeout: 5_000 });
  });

  test("leave channel returns the session to unjoined", async ({ page, request }) => {
    await seedFixture(request, teamSeed());

    await navigateToSession(page, SESSION_1_NAME);
    await createChannelFromUI(page, CHANNEL_NAME);
    await expectChannelMembership(page, true);

    await openOverflowMenu(page);
    await page.getByRole("menuitem", { name: "Leave channel" }).click();
    await expect(page.getByText("Left channel")).toBeVisible({ timeout: 5_000 });

    await expectChannelMembership(page, false);
  });
});

test.describe("Channel message routing", () => {
  // Delivery is deliberately asymmetric (see writeLegacyAgentMessageEvents and
  // tryLiveDelivery in session/channel.go): the sender keeps an inline
  // agent_message event so the call is visible in its own turn, while the
  // recipient's copy goes to the messages table and a channel.message WS event
  // — explicitly *not* into its transcript, so a routed message can never be
  // mistaken for something the user typed. This test locks both halves.
  test("SendMessage routes without polluting the recipient's transcript", async ({
    page,
    request,
  }) => {
    await seedFixture(request, teamSeed());

    await navigateToSession(page, SESSION_1_NAME);
    await createChannelFromUI(page, CHANNEL_NAME, "lead");
    await navigateToSession(page, SESSION_2_NAME);
    await joinChannelFromUI(page, "worker");

    // Run the lead's scenario, which calls SendMessage targeting the worker.
    const composer = await navigateToSession(page, SESSION_1_NAME);
    await sendQuery(page, composer, "Check app.ts and delegate tests");

    // Sender side: the delegation is part of the lead's own turn.
    await expect(page.getByText(DELEGATED_WORK)).toBeVisible({ timeout: 10_000 });
    await expect(page.getByText("Delegated to the worker")).toBeVisible({ timeout: 10_000 });
    await waitForState(request, SESSION_1_ID, "idle");

    // Recipient side: nothing lands in the worker's transcript.
    await navigateToSession(page, SESSION_2_NAME);
    await expect(page.getByText("Send a message to start chatting")).toBeVisible({
      timeout: 10_000,
    });
    await expect(page.getByText(DELEGATED_WORK)).toHaveCount(0);
  });
});
