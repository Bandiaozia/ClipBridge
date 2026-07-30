package com.clipbridge.app.crypto

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class SensitiveDetectorTest {
    @Test
    fun detectsTokensAndLeavesOrdinaryTextAlone() {
        assertTrue(SensitiveDetector.isSensitive("Bearer abcdefghijklmnopqrstuvwxyz"))
        assertTrue(SensitiveDetector.isSensitive("postgresql://user:pass@host/db"))
        assertFalse(SensitiveDetector.isSensitive("今天下午三点开会"))
    }
}
