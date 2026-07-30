package com.clipbridge.app.settings

import android.app.Application
import android.content.Context
import androidx.test.core.app.ApplicationProvider
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.annotation.Config

@RunWith(RobolectricTestRunner::class)
@Config(application = Application::class)
class SettingsStoreTest {
    private lateinit var context: Context

    @Before
    fun resetPreferences() {
        context = ApplicationProvider.getApplicationContext()
        context.getSharedPreferences("clipbridge_settings", Context.MODE_PRIVATE)
            .edit()
            .clear()
            .commit()
    }

    @Test
    fun `theme selection survives repository recreation`() {
        SettingsStore(context).themeColor = "green"

        assertEquals("green", SettingsStore(context).themeColor)
    }

    @Test
    fun `default theme remains understated blue`() {
        assertEquals("blue", SettingsStore(context).themeColor)
    }
}
