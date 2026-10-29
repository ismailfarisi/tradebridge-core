---
name: TradeBridge
description: A neutral B2B marketplace with a buyer-side negotiation agent, built for UAE/GCC small businesses.
colors:
  gulf-night-indigo: "#4338ca"
  gulf-night-indigo-deep: "#3730a3"
  indigo-mist: "#eef2ff"
  night-ink: "#0b1020"
  counting-house-gray: "#f6f7f9"
  paper-white: "#ffffff"
  shelf-gray: "#f2f4f7"
  hairline: "#e4e7ec"
  hairline-strong: "#d0d5dd"
  ledger-black: "#101828"
  slate-muted: "#475467"
  slate-faint: "#667085"
  deal-green: "#067647"
  deal-green-mist: "#ecfdf3"
  walkaway-red: "#d92d20"
  walkaway-red-mist: "#fef3f2"
  approval-amber: "#b54708"
  approval-amber-mist: "#fffaeb"
typography:
  display:
    fontFamily: "Inter, system-ui, -apple-system, Segoe UI, Roboto, Helvetica, Arial, sans-serif"
    fontSize: "3.3rem"
    fontWeight: 750
    lineHeight: 1.04
    letterSpacing: "-0.035em"
  headline:
    fontFamily: "Inter, system-ui, -apple-system, Segoe UI, Roboto, Helvetica, Arial, sans-serif"
    fontSize: "2.1rem"
    fontWeight: 720
    lineHeight: 1.15
    letterSpacing: "-0.025em"
  metric:
    fontFamily: "Inter, system-ui, -apple-system, Segoe UI, Roboto, Helvetica, Arial, sans-serif"
    fontSize: "1.75rem"
    fontWeight: 700
    letterSpacing: "-0.025em"
    fontFeature: "tnum"
  title:
    fontFamily: "Inter, system-ui, -apple-system, Segoe UI, Roboto, Helvetica, Arial, sans-serif"
    fontSize: "1.05rem"
    fontWeight: 650
    letterSpacing: "-0.01em"
  body:
    fontFamily: "Inter, system-ui, -apple-system, Segoe UI, Roboto, Helvetica, Arial, sans-serif"
    fontSize: "15px"
    fontWeight: 400
    lineHeight: 1.5
    fontFeature: "cv11, ss01"
  label:
    fontFamily: "Inter, system-ui, -apple-system, Segoe UI, Roboto, Helvetica, Arial, sans-serif"
    fontSize: "0.72rem"
    fontWeight: 600
    letterSpacing: "0.04em"
rounded:
  sm: "6px"
  md: "8px"
  lg: "12px"
  xl: "16px"
  pill: "999px"
spacing:
  xs: "0.35rem"
  sm: "0.75rem"
  md: "1rem"
  lg: "1.25rem"
  xl: "1.75rem"
components:
  button-primary:
    backgroundColor: "{colors.gulf-night-indigo}"
    textColor: "{colors.paper-white}"
    rounded: "{rounded.md}"
    padding: "0.55rem 1rem"
  button-primary-hover:
    backgroundColor: "{colors.gulf-night-indigo-deep}"
    textColor: "{colors.paper-white}"
  button-secondary:
    backgroundColor: "{colors.paper-white}"
    textColor: "{colors.ledger-black}"
    rounded: "{rounded.md}"
    padding: "0.55rem 1rem"
  button-secondary-hover:
    backgroundColor: "{colors.shelf-gray}"
  button-success:
    backgroundColor: "{colors.deal-green}"
    textColor: "{colors.paper-white}"
    rounded: "{rounded.md}"
    padding: "0.55rem 1rem"
  button-dark:
    backgroundColor: "{colors.night-ink}"
    textColor: "{colors.paper-white}"
    rounded: "{rounded.md}"
    padding: "0.55rem 1rem"
  input:
    backgroundColor: "{colors.paper-white}"
    textColor: "{colors.ledger-black}"
    rounded: "{rounded.md}"
    padding: "0.6rem 0.75rem"
  card:
    backgroundColor: "{colors.paper-white}"
    rounded: "{rounded.lg}"
    padding: "1.25rem 1.4rem"
  badge-success:
    backgroundColor: "{colors.deal-green-mist}"
    textColor: "{colors.deal-green}"
    rounded: "{rounded.pill}"
    padding: "0.15rem 0.55rem"
  badge-warning:
    backgroundColor: "{colors.approval-amber-mist}"
    textColor: "{colors.approval-amber}"
    rounded: "{rounded.pill}"
    padding: "0.15rem 0.55rem"
  sidebar:
    backgroundColor: "{colors.night-ink}"
    width: "248px"
  sidebar-link-active:
    textColor: "{colors.paper-white}"
    rounded: "{rounded.md}"
    padding: "0.5rem 0.6rem"
---

# Design System: TradeBridge

## Overview

**Creative North Star: "The Souk, Digitized"**

A Gulf trading floor is loud, crowded and full of offers. TradeBridge keeps the crowd (many suppliers, many quotes, many rounds of haggling) and takes away the noise. Every offer gets its own row, every counter shows its reason, and every dirham saved is visible. The energy comes from the volume of trade on screen, not from decoration.

The look is refined and restrained. White trading surfaces sit on a cool counting-house gray, under a night-ink sidebar that frames the floor. Hairline borders and near-flat shadows separate things; one indigo accent marks where the owner should act. Density is medium-high: tables, KPI strips and timelines carry the screen, and generous line-height keeps them readable for a busy owner glancing between tasks.

Numbers are the merchandise. Prices, savings and deltas use tabular figures, and outcomes use a strict status vocabulary: green for a closed deal, amber for awaiting the owner, red for walk-away.

**Key Characteristics:**
- Night-ink navigation frame around light, white working surfaces.
- One accent (Gulf Night Indigo) for primary actions, active nav and links.
- Hairline borders (1px) and whisper shadows instead of heavy elevation.
- Tabular figures for every price, saving and metric.
- Fixed status colors: deal green, approval amber, walk-away red.
- Inline SVG line icons (`web/components/Icon.tsx`), no emoji or icon fonts.

## Colors

A cool, near-neutral palette with one confident indigo accent and three semantic status hues.

### Primary
- **Gulf Night Indigo** (gulf-night-indigo): primary buttons, links, focus rings, active tab and nav accents, progress bars, the negotiation timeline's counter dots. The color of "act here" and "the agent is working".
- **Gulf Night Indigo, Deep** (gulf-night-indigo-deep): hover and pressed state of indigo elements only.
- **Indigo Mist** (indigo-mist): soft tint behind info badges, icon tiles, avatars, input focus halos and info banners.

### Neutral
- **Night Ink** (night-ink): the app sidebar, dark buttons and the closing marketing CTA. The frame of the trading floor.
- **Counting-House Gray** (counting-house-gray): the page background behind all cards.
- **Paper White** (paper-white): cards, tables, inputs, the top bar. Every working surface.
- **Shelf Gray** (shelf-gray): secondary fills such as tab tracks, quote bubbles, hover rows and notices.
- **Hairline** (hairline) / **Hairline Strong** (hairline-strong): card and table dividers, and control borders, in that order.
- **Ledger Black** (ledger-black): primary text and figures.
- **Slate Muted** (slate-muted): secondary text, labels, descriptions.
- **Slate Faint** (slate-faint): tertiary text, table headers, timestamps. It is the lightest gray that still meets WCAG AA on white; never go lighter for text.

### Status
- **Deal Green** (deal-green) on **Deal Green Mist**: completed deals, savings, "up" deltas, the approve action, live indicators.
- **Approval Amber** (approval-amber) on **Approval Amber Mist**: awaiting the owner's approval, warnings. The sidebar count pill uses a brighter amber (#f59e0b) to draw the eye.
- **Walk-away Red** (walkaway-red) on **Walk-away Red Mist**: walk-aways, errors, destructive actions (rendered in a darker #b42318 for text contrast).

### Named Rules
**The One Merchant Rule.** Indigo is the only brand hue in the app. Status colors carry meaning, not decoration; never use green, amber or red just for variety.

**The Status Vocabulary Rule.** Green means a deal closed or money saved, amber means the owner must act, red means no deal or an error. A color always means the same thing on every screen.

## Typography

**Body and Display Font:** Inter (loaded via `next/font`), falling back to system-ui / -apple-system / Segoe UI / Roboto.

**Character:** A single neutral grotesque used at many weights, so hierarchy comes from weight, size and tracking rather than contrasting families. Large text is tightened (negative tracking) and small labels are opened up.

Inter is self-hosted through `next/font/google` in `app/layout.tsx` and exposed as `--font-inter`; `globals.css` puts it first in the stack.

### Hierarchy
- **Display** (750, 3.3rem, line-height 1.04, -0.035em): marketing hero headline only; drops to 2.3rem under 900px.
- **Headline** (720, 2.1rem, -0.025em): marketing section titles.
- **Metric** (700, 1.75rem, -0.025em, tabular): KPI values on dashboards, Savings and Negotiations.
- **Title** (650, ~1.05rem, -0.01em): card and section headings in the app.
- **Body** (400, 15px, line-height 1.5): all running text and table cells (0.88rem in tables).
- **Label** (600, 0.72–0.78rem): form labels (muted), table headers and sidebar group labels (uppercase, +0.04–0.08em tracking).

### Named Rules
**The Tabular Money Rule.** Every price, saving, total and count uses tabular figures (`font-variant-numeric: tabular-nums`), so columns of offers line up like a ledger.

## Layout

The app shell is a fixed 248px night-ink sidebar beside a sticky 60px white top bar. Content sits in a column up to 1240px wide with 1.75rem padding (1.1rem under 860px, where the sidebar is hidden). Screens are composed top to bottom: page header, then a KPI strip (an auto-fit grid of tiles at least 200px wide, 1rem gap), then a main/secondary split (1.6fr / 1fr, stacking under 960px) of tables, timelines and leaderboards. Spacing follows a ~0.75rem / 1rem / 1.25rem rhythm. Marketing pages use a 1160px wrap with 4.5rem section padding and a two-column hero that stacks under 900px.

Breakpoints observed: 640px (stack small grids), 860px (hide sidebar), 900px (marketing stacks), 960px (app split stacks).

## Elevation & Depth

Mostly flat. Depth comes from tone (white surfaces on gray, night-ink frame) and 1px hairlines; shadows are barely there and add a touch of physicality, not height.

### Shadow Vocabulary
- **Whisper** (`box-shadow: 0 1px 2px rgba(16,24,40,0.05)`): cards, KPI tiles, inputs, buttons, the active tab.
- **Resting** (`box-shadow: 0 1px 3px rgba(16,24,40,0.08), 0 1px 2px rgba(16,24,40,0.04)`): occasional raised containers.
- **Lifted** (`box-shadow: 0 12px 32px -8px rgba(16,24,40,0.16)`): marketing showcase only: the hero demo card and the highlighted pricing tier.

### Named Rules
**The Flat Floor Rule.** In the app, the Whisper shadow is the ceiling. Lifted shadows belong to marketing showcase elements, never to working UI.

## Shapes

Gently rounded and consistent. Controls (buttons, inputs, tabs, nav links) use 8px corners; cards, KPI tiles and banners use 12px; showcase panels 16px; the closing CTA band 20px. Badges, avatars, timeline dots and progress bars are fully round pills. The focus outline uses a 6px radius. Borders are always 1px hairlines; the only 2px border is the highlighted pricing tier.

## Components

### Buttons
- **Shape:** gently rounded (8px), 600 weight, 0.9rem, with an inline-flex icon gap of 0.4rem.
- **Primary:** Gulf Night Indigo fill, white text; hover deepens to the deep indigo. One per view.
- **Secondary (default):** Paper White fill with a strong hairline border and Whisper shadow; hover fills Shelf Gray.
- **Success:** Deal Green fill; reserved for approving a deal (e.g. "Approve & create PO").
- **Dark:** Night Ink fill; secondary emphasis on marketing and onboarding.
- **Danger:** white fill with red text and a pale red border; hover adds the red mist.
- **Ghost / Small:** transparent with no border or shadow; small is 0.35rem × 0.7rem at 0.8rem text.
- **Focus:** 2px indigo outline, 2px offset. **Disabled:** 55% opacity.

### Badges
- **Style:** pill, 0.72rem, 600 weight, capitalized, 1px border in the status hue's light tint, filled with its mist.
- **Variants:** success, warning, danger, info (indigo), neutral (gray border, muted text).

### Cards / Containers
- **Corner Style:** 12px.
- **Background:** Paper White on Counting-House Gray.
- **Shadow Strategy:** Whisper only.
- **Border:** 1px Hairline.
- **Internal Padding:** 1.25rem × 1.4rem (KPI tiles 1.1rem × 1.2rem).

### Inputs / Fields
- **Style:** Paper White fill, strong hairline border, 8px corners, Whisper shadow, 0.92rem text; labels sit above in muted 0.78rem/600.
- **Focus:** border turns indigo with a 3px Indigo Mist halo; keyboard focus adds a 2px indigo outline.
- **Error:** inline darker-red 0.85rem message below the field.

### Navigation
- **Sidebar:** Night Ink, 248px, sticky. Uppercase faint group labels; links are 0.89rem/500 slate on ink. Hover adds a 6% white wash; the active link gets an indigo wash (22%) and a light-indigo icon. Live counts appear as amber pills. Hidden under 860px.
- **Top bar:** white, 60px, hairline bottom border, with 36px icon buttons and a round indigo-mist avatar.
- **Tabs / filters:** segmented control on a Shelf Gray track; the selected tab is white with a Whisper shadow.

### Data Table
- **Headers:** 0.72rem uppercase, +0.04em, Slate Faint, hairline under.
- **Rows:** 0.6rem padding, hairline separators, near-white hover; the primary cell is a bold ledger-black link that turns indigo on hover.

### KPI Tile (signature)
A label row with an icon, a large tabular Metric value, and a faint sub-line. Each screen may have at most one **accent tile**, filled with an indigo-to-teal gradient (#312e81 → #4338ca → #0891b2) with white text, for the headline number (e.g. total savings).

### Negotiation Timeline (signature)
A vertical ledger of rounds: 28px round dots (indigo for a counter, green for accept, red for walk-away) joined by a 2px hairline spine. Each item shows a meta line (supplier, round, time), a bold tabular price move ("AED 41,200 → 39,535"), and the agent's message in a Shelf Gray quote bubble. A pulsing green "Live" dot marks active negotiations.

## Do's and Don'ts

### Do:
- **Do** keep working surfaces Paper White (#ffffff) on Counting-House Gray (#f6f7f9), with 1px Hairline (#e4e7ec) borders.
- **Do** use Gulf Night Indigo (#4338ca) for exactly one primary action per view, plus links, focus and active states.
- **Do** set every price, saving and count in tabular figures, with the currency code shown (e.g. "AED 39,535").
- **Do** show what each negotiation round did and why: price move, status color, and the agent's reason.
- **Do** use inline SVG line icons from `web/components/Icon.tsx`, sized 16–20px.
- **Do** keep text at Slate Faint (#667085) or darker for AA contrast.

### Don't:
- **Don't** use emoji or glyph characters as icons; they were removed for rendering inconsistency.
- **Don't** introduce a second brand hue or use status colors as decoration (see The One Merchant Rule).
- **Don't** put Lifted shadows or hover lifts on app UI; elevation stays at Whisper.
- **Don't** add gradients beyond the three that exist (brand mark, single KPI accent tile, marketing hero accent and CTA band).
- **Don't** return to the old dark theme for app content areas; dark is reserved for the sidebar frame and the marketing CTA band.
