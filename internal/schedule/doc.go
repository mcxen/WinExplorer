// Package schedule holds timed job definitions, next-run math, schedules.json
// persistence, and the pure file-rule matching/planning used by the app's
// background scheduler. The UI and the process ticker live in package main;
// this package stays free of App / ui so the clock and dry-run logic can be
// tested without a window.
package schedule
