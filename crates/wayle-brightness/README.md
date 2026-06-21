<p align="center">
  <img src="https://raw.githubusercontent.com/stubbedev/wayle-services/master/assets/wayle-services.svg" width="200" alt="Wayle">
</p>

# wayle-brightness

Backlight control for internal and external (DDC/CI) displays with reactive state.

[![Crates.io](https://img.shields.io/crates/v/wayle-brightness)](https://crates.io/crates/wayle-brightness)
[![docs.rs](https://img.shields.io/docsrs/wayle-brightness)](https://docs.rs/wayle-brightness)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

```sh
cargo add wayle-brightness
```

## Usage

`BrightnessService::new()` returns `None` when no backlight devices are found. The `devices` field tracks every backlight device — internal panels and external DDC/CI monitors alike.

```rust,no_run
use wayle_brightness::{BrightnessService, Percentage};
use futures::StreamExt;

async fn example() -> Result<(), wayle_brightness::Error> {
    let Some(brightness) = BrightnessService::new().await? else {
        return Ok(());
    };

    for device in brightness.devices.get() {
        println!("{}: {}", device.name, device.percentage());
        device.set_percentage(Percentage::new(50.0)).await?;
    }

    let mut stream = brightness.devices.watch();
    while let Some(devices) = stream.next().await {
        for device in devices {
            println!("Brightness: {}", device.percentage());
        }
    }
    Ok(())
}
```

On non-systemd systems, direct sysfs writes require `video` group membership.

## License

MIT

Part of [wayle-services](https://github.com/stubbedev/wayle-services).
