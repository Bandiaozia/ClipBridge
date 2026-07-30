#pragma once

class AutoStartManager final {
public:
    [[nodiscard]] bool isEnabled() const;
    bool setEnabled(bool enabled) const;
};

