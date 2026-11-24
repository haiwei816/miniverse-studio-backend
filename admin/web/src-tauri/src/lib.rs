use tauri::{
    menu::{Menu, MenuItem, PredefinedMenuItem},
    tray::{MouseButton, MouseButtonState, TrayIconBuilder, TrayIconEvent},
    AppHandle, Emitter, Manager,
};
use tauri_plugin_global_shortcut::{Code, GlobalShortcutExt, Modifiers, Shortcut, ShortcutState};

fn reveal(app: &AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let _ = window.show();
        let _ = window.unminimize();
        let _ = window.set_focus();
    }
}

// The hotkey and the tray both behave like a menu-bar utility: pressing again
// puts the window away instead of stacking it behind whatever is focused.
fn toggle(app: &AppHandle) {
    if let Some(window) = app.get_webview_window("main") {
        let visible = window.is_visible().unwrap_or(false);
        let focused = window.is_focused().unwrap_or(false);
        if visible && focused {
            let _ = window.hide();
        } else {
            reveal(app);
        }
    }
}

fn action(app: &AppHandle, name: &str) {
    reveal(app);
    let _ = app.emit("gm://action", name);
}

pub fn run() {
    #[cfg(target_os = "macos")]
    let (shortcut, accel) = (
        Shortcut::new(Some(Modifiers::SUPER | Modifiers::SHIFT), Code::KeyG),
        "Cmd+Shift+G",
    );
    #[cfg(not(target_os = "macos"))]
    let (shortcut, accel) = (
        Shortcut::new(Some(Modifiers::CONTROL | Modifiers::SHIFT), Code::KeyG),
        "Ctrl+Shift+G",
    );

    tauri::Builder::default()
        .plugin(tauri_plugin_http::init())
        .plugin(tauri_plugin_notification::init())
        .plugin(
            tauri_plugin_global_shortcut::Builder::new()
                .with_handler(move |app, _shortcut, event| {
                    if event.state() == ShortcutState::Pressed {
                        toggle(app);
                    }
                })
                .build(),
        )
        .on_window_event(|window, event| {
            if let tauri::WindowEvent::CloseRequested { api, .. } = event {
                if window.label() == "main" {
                    api.prevent_close();
                    let _ = window.hide();
                }
            }
        })
        .setup(move |app| {
            let open = MenuItem::with_id(app, "open", "打开控制台", true, Some(accel))?;
            let award = MenuItem::with_id(app, "award", "补发奖励…", true, None::<&str>)?;
            let refresh = MenuItem::with_id(app, "refresh", "刷新数据", true, None::<&str>)?;
            let separator = PredefinedMenuItem::separator(app)?;
            let quit = MenuItem::with_id(app, "quit", "退出 MiniVerse GM", true, None::<&str>)?;
            let menu = Menu::with_items(app, &[&open, &award, &refresh, &separator, &quit])?;

            let mut tray = TrayIconBuilder::with_id("main")
                .tooltip("MiniVerse GM")
                .menu(&menu)
                .show_menu_on_left_click(false)
                .on_menu_event(|app, event| match event.id.as_ref() {
                    "open" => reveal(app),
                    "award" => action(app, "award"),
                    "refresh" => action(app, "refresh"),
                    "quit" => app.exit(0),
                    _ => {}
                })
                .on_tray_icon_event(|tray, event| {
                    if let TrayIconEvent::Click {
                        button: MouseButton::Left,
                        button_state: MouseButtonState::Up,
                        ..
                    } = event
                    {
                        toggle(tray.app_handle());
                    }
                });
            if let Some(icon) = app.default_window_icon() {
                tray = tray.icon(icon.clone());
            }
            tray.build(app)?;

            app.global_shortcut().register(shortcut)?;
            Ok(())
        })
        .run(tauri::generate_context!())
        .expect("failed to start MiniVerse GM");
}
