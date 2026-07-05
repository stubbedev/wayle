use std::sync::Arc;

use wayle_audio::{
    Error,
    core::device::{input::InputDevice, output::OutputDevice},
    volume::types::Volume,
};

use crate::shell::bar::dropdowns::audio::helpers;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum VolumeSectionKind {
    Output,
    Input,
}

pub struct VolumeSectionInit {
    pub audio: Arc<wayle_audio::AudioService>,
    pub kind: VolumeSectionKind,
}

#[derive(Debug)]
pub enum VolumeSectionInput {
    VolumeCommitted(f64),
    MuteClicked,
    ShowDevicesClicked,
}

#[derive(Debug)]
pub enum VolumeSectionCmd {
    DeviceChanged(Option<ActiveDevice>),
    VolumeOrMuteChanged,
}

#[derive(Debug)]
pub enum VolumeSectionOutput {
    ShowDevices,
    HasDeviceChanged(bool),
}

#[derive(Debug, Clone)]
pub enum ActiveDevice {
    Output(Arc<OutputDevice>),
    Input(Arc<InputDevice>),
}

impl ActiveDevice {
    pub fn volume_percentage(&self) -> f64 {
        match self {
            Self::Output(device) => device.volume.get().average_percentage(),
            Self::Input(device) => device.volume.get().average_percentage(),
        }
    }

    pub fn muted(&self) -> bool {
        match self {
            Self::Output(device) => device.muted.get(),
            Self::Input(device) => device.muted.get(),
        }
    }

    pub fn description(&self) -> String {
        match self {
            Self::Output(device) => device.description.get(),
            Self::Input(device) => device.description.get(),
        }
    }

    pub fn trigger_icon(&self) -> &'static str {
        match self {
            Self::Output(device) => helpers::output_device_icon(
                &device.name.get(),
                &device.description.get(),
                &device.properties.get(),
            ),
            Self::Input(device) => helpers::input_device_icon(
                &device.name.get(),
                &device.description.get(),
                &device.properties.get(),
            ),
        }
    }

    pub fn channels(&self) -> usize {
        match self {
            Self::Output(device) => device.volume.get().channels(),
            Self::Input(device) => device.volume.get().channels(),
        }
    }

    pub async fn set_volume(&self, volume: Volume) -> Result<(), Error> {
        match self {
            Self::Output(device) => device.set_volume(volume).await,
            Self::Input(device) => device.set_volume(volume).await,
        }
    }

    pub async fn set_mute(&self, muted: bool) -> Result<(), Error> {
        match self {
            Self::Output(device) => device.set_mute(muted).await,
            Self::Input(device) => device.set_mute(muted).await,
        }
    }
}
