//! Per-module settings pages. Each module exports an `entry()` returning a `LeafEntry`.

mod battery;
mod bluetooth;
mod brightness;
mod cava;
mod clock;
mod cpu;
mod custom;
mod dashboard;
mod hyprland_workspaces;
mod hyprsunset;
mod idle_inhibit;
mod keybind_mode;
mod keyboard_input;
mod mail;
mod mango_workspaces;
mod media;
mod microphone;
mod netstat;
mod network;
mod niri_workspaces;
mod notification_module;
mod power;
mod power_profiles;
mod ram;
mod recorder;
mod screenshot;
mod separator;
mod storage;
mod sway_workspaces;
mod systray;
mod treeman;
mod volume;
mod weather;
mod window_title;
mod world_clock;

use wayle_config::Config;

use super::nav::LeafEntry;

pub(crate) fn factories() -> Vec<fn(&Config) -> LeafEntry> {
    vec![
        battery::entry,
        bluetooth::entry,
        brightness::entry,
        cava::entry,
        clock::entry,
        cpu::entry,
        custom::entry,
        dashboard::entry,
        hyprland_workspaces::entry,
        hyprsunset::entry,
        idle_inhibit::entry,
        keybind_mode::entry,
        keyboard_input::entry,
        mail::entry,
        mango_workspaces::entry,
        media::entry,
        microphone::entry,
        netstat::entry,
        network::entry,
        niri_workspaces::entry,
        notification_module::entry,
        power::entry,
        power_profiles::entry,
        ram::entry,
        recorder::entry,
        screenshot::entry,
        separator::entry,
        storage::entry,
        sway_workspaces::entry,
        systray::entry,
        treeman::entry,
        volume::entry,
        weather::entry,
        window_title::entry,
        world_clock::entry,
    ]
}
